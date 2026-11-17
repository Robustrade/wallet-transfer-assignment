package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/dto"
	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CreateTransfer runs one wallet-to-wallet movement inside a single DB transaction.
//
// Flow (all-or-nothing on commit):
//  1. Validate business input (amount > 0, distinct wallets).
//  2. Claim the idempotency key before touching balances.
//  3. Lock both wallets in id order (avoids A↔B deadlocks).
//  4. Debit source only if funds exist (no double-spend under concurrency).
//  5. On success: credit destination + write double-entry ledger + mark PROCESSED.
//     On shortage: mark FAILED, leave balances/ledger unchanged.
//  6. Store the HTTP response on the idempotency row, then commit.
func (s *Service) CreateTransfer(ctx context.Context, idempotencyKey, fromWalletID, toWalletID string, amount int64) (dto.TransferOutcome, error) {
	from, to, amt, claim, err := prepareTransferRequest(idempotencyKey, fromWalletID, toWalletID, amount)
	if err != nil {
		return dto.TransferOutcome{}, err
	}

	txCtx, tx, err := repository.BeginTx(ctx, s.pool)
	if err != nil {
		return dto.TransferOutcome{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit

	claimed, err := s.idem.Claim(txCtx, claim)
	if err != nil {
		return dto.TransferOutcome{}, err
	}
	if !claimed {
		return s.replayOrMismatch(txCtx, tx, claim.IdempotencyKey, claim.RequestHash)
	}

	fromWallet, toWallet, err := s.lockTransferWallets(txCtx, from, to)
	if err != nil {
		return dto.TransferOutcome{}, err
	}
	// Reject before debit so an overflowing credit cannot leave a partial balance move
	// (or a Postgres bigint overflow → 500) inside this transaction.
	if !toWallet.CanCredit(amt) {
		return dto.TransferOutcome{}, model.ErrBalanceOverflow
	}

	tr, err := s.insertPendingTransfer(txCtx, claim.IdempotencyKey, from, to, amt)
	if err != nil {
		return dto.TransferOutcome{}, err
	}

	debited, err := s.debitIfFundsAvailable(txCtx, fromWallet, tr)
	if err != nil {
		return dto.TransferOutcome{}, err
	}
	if !debited {
		return s.commitFailedInsufficient(txCtx, tx, tr)
	}

	return s.commitProcessedTransfer(ctx, txCtx, tx, tr)
}

// GetTransfer loads a transfer by id or ErrTransferNotFound.
func (s *Service) GetTransfer(ctx context.Context, id uuid.UUID) (dto.TransferView, error) {
	t, err := s.transfers.GetByID(ctx, id)
	if errors.Is(err, model.ErrNotFound) {
		return dto.TransferView{}, ErrTransferNotFound
	}
	if err != nil {
		return dto.TransferView{}, err
	}
	return dto.NewTransferView(t), nil
}

// prepareTransferRequest validates input and builds the idempotency claim (hash + body).
func prepareTransferRequest(idempotencyKey, fromWalletID, toWalletID string, amount int64) (from, to string, amt helpers.Money, claim *model.IdempotencyRecord, err error) {
	from, to, amt, err = validateTransferInput(fromWalletID, toWalletID, amount)
	if err != nil {
		return "", "", 0, nil, err
	}
	hash, body, err := model.HashTransferRequest(from, to, amt.Int64())
	if err != nil {
		return "", "", 0, nil, err
	}
	claim, err = model.NewIdempotencyRecord(idempotencyKey, hash, body)
	if err != nil {
		return "", "", 0, nil, err
	}
	return from, to, amt, claim, nil
}

// lockTransferWallets takes row-level locks (SELECT … FOR UPDATE) on both wallets
// in id order so concurrent debits of the same wallet serialize and A↔B cannot deadlock.
func (s *Service) lockTransferWallets(ctx context.Context, from, to string) (fromWallet, toWallet *model.Wallet, err error) {
	locked, err := s.wallets.LockByIDs(ctx, from, to)
	if err != nil {
		return nil, nil, err
	}
	if len(locked) != 2 {
		// Unknown wallet: caller rolls back so the idempotency key is not consumed.
		return nil, nil, ErrWalletNotFound
	}
	fromWallet, err = walletByID(locked, from)
	if err != nil {
		return nil, nil, err
	}
	toWallet, err = walletByID(locked, to)
	if err != nil {
		return nil, nil, err
	}
	return fromWallet, toWallet, nil
}

// insertPendingTransfer creates a PENDING transfer row.
func (s *Service) insertPendingTransfer(ctx context.Context, key, from, to string, amt helpers.Money) (*model.Transfer, error) {
	tr, err := model.NewTransfer(key, from, to, amt.Int64())
	if err != nil {
		return nil, err
	}
	if err := assertTransferReadyToPersist(tr); err != nil {
		return nil, err
	}
	if err := s.transfers.Insert(ctx, tr); err != nil {
		return nil, err
	}
	return tr, nil
}

// debitIfFundsAvailable debits the source under the row lock already held by lockTransferWallets.
// A second concurrent transfer waits on that lock, then sees the updated balance — no double-spend.
// debited=false means insufficient funds (no balance change).
func (s *Service) debitIfFundsAvailable(ctx context.Context, fromWallet *model.Wallet, tr *model.Transfer) (debited bool, err error) {
	if err := assertAmountForBalanceUpdate(tr.Amount); err != nil {
		return false, err
	}
	if !fromWallet.CanDebit(tr.Amount) {
		return false, nil
	}
	ok, err := s.wallets.Debit(ctx, tr.FromWalletID, tr.Amount)
	if err != nil {
		return false, err
	}
	return ok, nil
}

// commitProcessedTransfer credits destination, writes the ledger pair, stores 201, and commits.
func (s *Service) commitProcessedTransfer(parent context.Context, txCtx context.Context, tx pgx.Tx, tr *model.Transfer) (dto.TransferOutcome, error) {
	if err := assertAmountForBalanceUpdate(tr.Amount); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := s.wallets.Credit(txCtx, tr.ToWalletID, tr.Amount); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := tr.MarkProcessed(); err != nil {
		return dto.TransferOutcome{}, err
	}

	debit, credit, err := model.NewLedgerPair(tr)
	if err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := assertConsistentLedgerPair(tr, debit, credit); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := s.ledger.InsertPair(txCtx, debit, credit); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := s.transfers.UpdateStatus(txCtx, tr); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := s.assertBalancesMatchLedger(txCtx, tr.FromWalletID, tr.ToWalletID); err != nil {
		return dto.TransferOutcome{}, err
	}

	return s.storeOutcomeAndCommit(parent, txCtx, tx, tr, 201)
}

// replayOrMismatch handles a duplicate idempotency key.
// Same hash → stored response. Different hash → mismatch. Rolls back (no second transfer).
func (s *Service) replayOrMismatch(txCtx context.Context, tx pgx.Tx, key, hash string) (dto.TransferOutcome, error) {
	stored, err := s.idem.Get(txCtx, key)
	if err != nil {
		return dto.TransferOutcome{}, err
	}
	_ = tx.Rollback(txCtx) // close the claim attempt; we only read
	if stored.RequestHash != hash {
		return dto.TransferOutcome{}, ErrIdempotencyMismatch
	}
	if !stored.HasResponse() {
		return dto.TransferOutcome{}, ErrInvariantBroken
	}
	return dto.TransferOutcome{
		StatusCode: *stored.ResponseCode,
		ReplayBody: append(json.RawMessage(nil), stored.ResponseBody...),
	}, nil
}

// commitFailedInsufficient records FAILED + 422 and commits (key stays consumed; no ledger).
func (s *Service) commitFailedInsufficient(txCtx context.Context, tx pgx.Tx, tr *model.Transfer) (dto.TransferOutcome, error) {
	if err := tr.MarkFailed(model.FailureInsufficientFunds); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := s.transfers.UpdateStatus(txCtx, tr); err != nil {
		return dto.TransferOutcome{}, err
	}
	return s.storeOutcomeAndCommit(txCtx, txCtx, tx, tr, 422)
}

// storeOutcomeAndCommit persists the idempotent HTTP response and commits the transfer tx.
func (s *Service) storeOutcomeAndCommit(parent, txCtx context.Context, tx pgx.Tx, tr *model.Transfer, statusCode int) (dto.TransferOutcome, error) {
	view := dto.NewTransferView(tr)
	payload, err := json.Marshal(view)
	if err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := s.idem.StoreResponse(txCtx, tr.IdempotencyKey, statusCode, payload); err != nil {
		return dto.TransferOutcome{}, err
	}
	if err := tx.Commit(parent); err != nil {
		return dto.TransferOutcome{}, fmt.Errorf("commit: %w", err)
	}
	return dto.TransferOutcome{StatusCode: statusCode, Transfer: &view}, nil
}

func walletByID(wallets []*model.Wallet, id string) (*model.Wallet, error) {
	for _, w := range wallets {
		if w.ID == id {
			return w, nil
		}
	}
	return nil, ErrWalletNotFound
}

// assertBalancesMatchLedger verifies balance = opening_balance + Σ(CREDIT) − Σ(DEBIT).
func (s *Service) assertBalancesMatchLedger(ctx context.Context, walletIDs ...string) error {
	seen := make(map[string]struct{}, len(walletIDs))
	for _, id := range walletIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		w, err := s.wallets.GetByID(ctx, id)
		if err != nil {
			return err
		}
		net, err := s.ledger.SumSigned(ctx, id)
		if err != nil {
			return err
		}
		if w.Balance.Int64() != w.OpeningBalance.Int64()+net {
			return ErrInvariantBroken
		}
	}
	return nil
}
