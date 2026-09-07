package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// errKeyAlreadyClaimed is an internal signal that another request already owns
// this idempotency key. It aborts the current transaction so the caller can
// replay the stored result from outside any transaction.
var errKeyAlreadyClaimed = errors.New("idempotency key already claimed")

// TransferResult is the outcome of CreateTransfer.
type TransferResult struct {
	Transfer *domain.Transfer
	// Replayed is true when the result came from a previously stored transfer
	// rather than fresh execution (duplicate idempotency key).
	Replayed bool
}

// Repositories bundles the persistence ports the service depends on.
type Repositories struct {
	Wallets     WalletRepository
	Transfers   TransferRepository
	Ledger      LedgerRepository
	Idempotency IdempotencyRepository
}

// TransferService orchestrates the transfer workflow: idempotency handling,
// the transactional boundary, wallet locking, ledger writes and state
// transitions. It contains no transport or SQL concerns.
type TransferService struct {
	repos Repositories
	tx    TxManager
	ids   IDGenerator
	log   *slog.Logger
}

// NewTransferService wires the service. log may be nil.
func NewTransferService(repos Repositories, tx TxManager, ids IDGenerator, log *slog.Logger) *TransferService {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &TransferService{repos: repos, tx: tx, ids: ids, log: log}
}

// CreateTransfer executes a wallet-to-wallet transfer with exactly-once
// semantics keyed by cmd.IdempotencyKey.
//
// Guarantees:
//   - a repeated key returns the original result and triggers no new side effects
//   - a key reused with a different payload returns ErrIdempotencyKeyConflict
//   - execution is atomic: either the two ledger entries and both balance
//     updates commit together, or nothing does
//   - concurrent debits of the same wallet are serialised by a row-level lock
//   - insufficient funds is a committed FAILED transfer, not a lost request
func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferCommand) (TransferResult, error) {
	cmd := in.normalized()
	if err := cmd.validate(); err != nil {
		return TransferResult{}, err
	}
	amount, err := cmd.money()
	if err != nil {
		return TransferResult{}, err
	}
	hash := cmd.requestHash()

	// Fast path: a key we have already seen is replayed without opening a
	// transaction. This is an optimisation only; the unique constraints inside
	// the transaction are the actual guarantee.
	if replay, ok, err := s.tryReplay(ctx, cmd.IdempotencyKey, hash); err != nil || ok {
		return replay, err
	}

	var result TransferResult
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		r, txErr := s.execute(ctx, cmd, amount, hash)
		result = r
		return txErr
	})
	if errors.Is(err, errKeyAlreadyClaimed) {
		return s.replayAfterRace(ctx, cmd.IdempotencyKey, hash)
	}
	if err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

// tryReplay returns the stored result for an idempotency key, if one exists.
// ok is false when the key has never been seen. A payload that does not match
// the stored request hash yields ErrIdempotencyKeyConflict.
func (s *TransferService) tryReplay(ctx context.Context, key, hash string) (TransferResult, bool, error) {
	transfer, err := s.repos.Transfers.GetByIdempotencyKey(ctx, key)
	if errors.Is(err, domain.ErrTransferNotFound) {
		return TransferResult{}, false, nil
	}
	if err != nil {
		return TransferResult{}, false, err
	}
	if rec, recErr := s.repos.Idempotency.GetRecord(ctx, key); recErr == nil && rec.RequestHash != hash {
		return TransferResult{}, false, domain.ErrIdempotencyKeyConflict
	}
	return TransferResult{Transfer: transfer, Replayed: true}, true, nil
}

// replayAfterRace resolves the result once a concurrent request has won the
// race for the idempotency key. Its transaction has committed by the time our
// claim was rejected.
func (s *TransferService) replayAfterRace(ctx context.Context, key, hash string) (TransferResult, error) {
	replay, ok, err := s.tryReplay(ctx, key, hash)
	if err != nil {
		return TransferResult{}, err
	}
	if !ok {
		return TransferResult{}, fmt.Errorf("idempotency key claimed but no transfer found: %w", domain.ErrTransferNotFound)
	}
	return replay, nil
}

// execute runs inside the transaction opened by CreateTransfer.
func (s *TransferService) execute(ctx context.Context, cmd CreateTransferCommand, amount domain.Money, hash string) (TransferResult, error) {
	transfer, err := domain.NewTransfer(s.ids.NewID(), cmd.IdempotencyKey, cmd.FromWalletID, cmd.ToWalletID, amount)
	if err != nil {
		return TransferResult{}, err
	}

	// Lock both wallet rows first, in a deterministic order. Doing this before
	// the transfer INSERT matters: the INSERT takes FOR KEY SHARE locks on the
	// wallets it references, and taking FOR UPDATE afterwards would be a lock
	// upgrade that deadlocks under concurrency. Locking up front also serialises
	// concurrent debits of the same wallet.
	from, to, err := s.lockWalletsOrdered(ctx, cmd.FromWalletID, cmd.ToWalletID)
	if err != nil {
		return TransferResult{}, err
	}
	if err := assertSameCurrency(amount, from, to); err != nil {
		return TransferResult{}, err
	}

	if err := s.claimKey(ctx, transfer, hash); err != nil {
		return TransferResult{}, err
	}

	return s.apply(ctx, transfer, from, to, amount)
}

// claimKey creates the PENDING transfer (whose unique idempotency_key is the
// claim) and records the request fingerprint.
func (s *TransferService) claimKey(ctx context.Context, transfer *domain.Transfer, hash string) error {
	if err := s.repos.Transfers.Create(ctx, transfer); err != nil {
		return mapClaimError(err)
	}
	err := s.repos.Idempotency.Insert(ctx, IdempotencyRecord{
		Key: transfer.IdempotencyKey, TransferID: transfer.ID, RequestHash: hash,
	})
	if err != nil {
		return mapClaimError(err)
	}
	return nil
}

func mapClaimError(err error) error {
	if errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		return errKeyAlreadyClaimed
	}
	return err
}

// apply moves the funds, writes the ledger pair, and settles the transfer
// state. Insufficient funds is committed as a FAILED transfer.
func (s *TransferService) apply(ctx context.Context, transfer *domain.Transfer, from, to *domain.Wallet, amount domain.Money) (TransferResult, error) {
	if err := from.Debit(amount); err != nil {
		if errors.Is(err, domain.ErrInsufficientFunds) {
			return s.failTransfer(ctx, transfer, "insufficient funds")
		}
		return TransferResult{}, err
	}
	if err := to.Credit(amount); err != nil {
		return TransferResult{}, err
	}
	if err := s.persistApplied(ctx, transfer, from, to, amount); err != nil {
		return TransferResult{}, err
	}

	s.log.InfoContext(ctx, "transfer processed",
		"transfer_id", transfer.ID, "idempotency_key", transfer.IdempotencyKey,
		"from", from.ID, "to", to.ID, "amount", amount.String())
	return TransferResult{Transfer: transfer}, nil
}

// persistApplied writes the balance updates, the debit/credit ledger pair, and
// the PROCESSED state within the current transaction.
func (s *TransferService) persistApplied(ctx context.Context, transfer *domain.Transfer, from, to *domain.Wallet, amount domain.Money) error {
	if err := s.repos.Wallets.UpdateBalance(ctx, from); err != nil {
		return err
	}
	if err := s.repos.Wallets.UpdateBalance(ctx, to); err != nil {
		return err
	}

	debit, credit, err := domain.NewLedgerPair(transfer.ID, from.ID, to.ID, amount)
	if err != nil {
		return err
	}
	if err := s.repos.Ledger.InsertPair(ctx, debit, credit); err != nil {
		return err
	}

	if err := transfer.MarkProcessed(); err != nil {
		return err
	}
	return s.repos.Transfers.UpdateState(ctx, transfer)
}

// failTransfer commits the transfer in the FAILED state. No balances or ledger
// entries are written, but the transfer and idempotency records persist so a
// retry replays the same failure rather than re-running.
func (s *TransferService) failTransfer(ctx context.Context, transfer *domain.Transfer, reason string) (TransferResult, error) {
	if err := transfer.MarkFailed(reason); err != nil {
		return TransferResult{}, err
	}
	if err := s.repos.Transfers.UpdateState(ctx, transfer); err != nil {
		return TransferResult{}, err
	}
	s.log.InfoContext(ctx, "transfer failed",
		"transfer_id", transfer.ID, "idempotency_key", transfer.IdempotencyKey, "reason", reason)
	return TransferResult{Transfer: transfer}, nil
}

// lockWalletsOrdered locks both wallet rows in ascending id order to avoid
// deadlocks between opposing transfers, then returns them as (from, to).
func (s *TransferService) lockWalletsOrdered(ctx context.Context, fromID, toID string) (from, to *domain.Wallet, err error) {
	firstID, secondID := fromID, toID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}

	first, err := s.repos.Wallets.GetForUpdate(ctx, firstID)
	if err != nil {
		return nil, nil, err
	}
	second, err := s.repos.Wallets.GetForUpdate(ctx, secondID)
	if err != nil {
		return nil, nil, err
	}

	if first.ID == fromID {
		return first, second, nil
	}
	return second, first, nil
}

func assertSameCurrency(amount domain.Money, wallets ...*domain.Wallet) error {
	for _, w := range wallets {
		if w.Balance.Currency != amount.Currency {
			return fmt.Errorf("%w: wallet %s is %s, transfer is %s",
				domain.ErrCurrencyMismatch, w.ID, w.Balance.Currency, amount.Currency)
		}
	}
	return nil
}
