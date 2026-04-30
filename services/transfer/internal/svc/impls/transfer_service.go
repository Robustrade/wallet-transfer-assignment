// Package svcimpl is the concrete implementation of the transfer service contract.
package svcimpl

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/response"
	repoiface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/repo/iface"
	svciface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/svc/ifaces"
)

// transferService orchestrates idempotency, locking, validation, ledger
// posting, and balance updates inside a single transaction.
type transferService struct {
	tx        repoiface.TxManager
	wallets   repoiface.WalletRepoIface
	transfers repoiface.TransferRepoIface
	ledger    repoiface.LedgerRepoIface
	logger    *slog.Logger
}

// Deps groups the constructor dependencies for clarity.
type Deps struct {
	TxManager    repoiface.TxManager
	WalletRepo   repoiface.WalletRepoIface
	TransferRepo repoiface.TransferRepoIface
	LedgerRepo   repoiface.LedgerRepoIface
	Logger       *slog.Logger
}

// NewTransferService constructs the transfer service.
func NewTransferService(d Deps) svciface.TransferServiceIface {
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &transferService{
		tx:        d.TxManager,
		wallets:   d.WalletRepo,
		transfers: d.TransferRepo,
		ledger:    d.LedgerRepo,
		logger:    logger,
	}
}

// CreateTransfer implements TransferServiceIface.
func (s *transferService) CreateTransfer(
	ctx context.Context,
	req request.CreateTransferReq,
) (response.TransferResp, error) {
	if err := req.Validate(); err != nil {
		return response.TransferResp{}, err
	}

	requestHash := hashRequest(req)

	var resp response.TransferResp
	err := s.tx.Run(ctx, func(ctx context.Context) error {
		return s.runInsideTx(ctx, req, requestHash, &resp)
	})
	if err != nil {
		return response.TransferResp{}, err
	}
	return resp, nil
}

// runInsideTx claims (or replays) the idempotency key and either returns the
// previously-committed result or processes the new transfer end-to-end.
func (s *transferService) runInsideTx(
	ctx context.Context,
	req request.CreateTransferReq,
	requestHash string,
	resp *response.TransferResp,
) error {
	t, replayed, err := s.claimOrReplay(ctx, req, requestHash)
	if err != nil {
		return err
	}
	if replayed {
		s.logger.InfoContext(ctx, "transfer idempotent replay",
			"idempotency_key", req.IdempotencyKey,
			"transfer_id", t.ID,
			"original_status", t.Status,
		)
		*resp = response.FromTransfer(*t, true)
		return nil
	}
	return s.processNewTransfer(ctx, t, req, resp)
}

// processNewTransfer locks both wallets, runs domain checks, and either
// commits a FAILED outcome or writes the ledger and updates balances.
func (s *transferService) processNewTransfer(
	ctx context.Context,
	t *model.Transfer,
	req request.CreateTransferReq,
	resp *response.TransferResp,
) error {
	// Lock both wallets in lex order via a single round-trip.
	wallets, err := s.wallets.LockByIDs(ctx, []string{req.FromWalletID, req.ToWalletID})
	if err != nil {
		return err
	}
	from, to, ok := wallets.Pick(req.FromWalletID, req.ToWalletID)
	if !ok {
		return apperr.ErrWalletNotFound
	}

	// Domain checks. Insufficient funds and currency mismatch are business
	// outcomes — they commit a FAILED row so subsequent replays return the
	// same error rather than re-attempting.
	if from.Currency != to.Currency {
		return s.markFailedAndCommit(ctx, *t, from.Currency, model.ReasonCurrencyMismatch, resp)
	}
	if from.Balance.Lt(req.AmountMoney()) {
		return s.markFailedAndCommit(ctx, *t, from.Currency, model.ReasonInsufficientFunds, resp)
	}

	t.Currency = from.Currency
	if err := s.applyAndComplete(ctx, t, from.ID, to.ID, req.AmountMoney()); err != nil {
		return err
	}
	*resp = response.FromTransfer(*t, false)
	return nil
}

// applyAndComplete writes ledger entries, applies balance deltas, and marks
// the transfer PROCESSED in storage.
func (s *transferService) applyAndComplete(
	ctx context.Context,
	t *model.Transfer,
	fromID, toID string,
	amount money.Money,
) error {
	if err := s.ledger.Append(ctx, t.LedgerEntries()); err != nil {
		return err
	}
	if err := s.wallets.ApplyBalanceDeltas(ctx, []repoiface.BalanceDelta{
		{WalletID: fromID, Delta: amount.Neg()},
		{WalletID: toID, Delta: amount},
	}); err != nil {
		return err
	}
	if !t.MarkProcessed() {
		return apperr.ErrInternal.WithMessage("invalid state transition to PROCESSED")
	}
	updatedAt, err := s.transfers.UpdateOutcome(ctx, t.ID, t.Status, t.Currency, nil)
	if err != nil {
		return err
	}
	t.UpdatedAt = updatedAt
	return nil
}

// claimOrReplay either inserts a new PENDING transfer or, on key collision,
// returns the existing committed transfer. A request_hash mismatch on a
// duplicate key returns ErrIdempotencyConflict.
//
// Returns (transfer, replayed, err). When replayed is true, transfer is the
// previously-committed row; when false, transfer is the freshly-inserted
// PENDING row with DB-populated timestamps.
func (s *transferService) claimOrReplay(
	ctx context.Context,
	req request.CreateTransferReq,
	requestHash string,
) (*model.Transfer, bool, error) {
	pending := model.NewPendingTransfer(
		uuid.New(),
		req.IdempotencyKey,
		requestHash,
		req.FromWalletID,
		req.ToWalletID,
		"", // currency is filled in after we lock the wallets
		req.AmountMoney(),
	)

	claimed, transfer, err := s.transfers.Claim(ctx, pending)
	if err != nil {
		return nil, false, err
	}
	if transfer == nil {
		return nil, false, apperr.ErrInternal.WithMessage("claim returned nil transfer")
	}
	if claimed {
		return transfer, false, nil
	}
	if transfer.RequestHash != requestHash {
		s.logger.WarnContext(ctx, "idempotency key reused with different body",
			"idempotency_key", req.IdempotencyKey,
			"existing_transfer_id", transfer.ID,
		)
		return nil, false, apperr.ErrIdempotencyConflict
	}
	return transfer, true, nil
}

// markFailedAndCommit transitions the transfer to FAILED, persists the change
// (including the canonical currency), and writes the response. Returning nil
// from the transaction closure causes the FAILED row to be committed, so
// duplicate retries see the same outcome.
func (s *transferService) markFailedAndCommit(
	ctx context.Context,
	t model.Transfer,
	currency string,
	reason model.FailureReason,
	out *response.TransferResp,
) error {
	if !t.MarkFailed(reason) {
		return apperr.ErrInternal.WithMessage("invalid state transition to FAILED")
	}
	t.Currency = currency
	updatedAt, err := s.transfers.UpdateOutcome(ctx, t.ID, t.Status, t.Currency, t.FailureReason)
	if err != nil {
		return err
	}
	t.UpdatedAt = updatedAt
	s.logger.WarnContext(ctx, "transfer failed",
		"transfer_id", t.ID,
		"idempotency_key", t.IdempotencyKey,
		"reason", string(reason),
		"from_wallet_id", t.FromWalletID,
		"to_wallet_id", t.ToWalletID,
		"amount_minor", t.Amount.Minor(),
	)
	*out = response.FromTransfer(t, false)
	return nil
}
