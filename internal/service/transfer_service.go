// Package service implements the transfer workflow: idempotency, the funds check,
// balance updates and ledger entries, all inside one database transaction.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
	"github.com/celinagupta/wallet-transfer-assignment/internal/repository"
)

// Result is the outcome of CreateTransfer. Replayed is true when the transfer was
// created by an earlier request with the same idempotency key and is returned as-is.
type Result struct {
	Transfer domain.Transfer
	Replayed bool
}

type TransferService struct {
	tx     repository.TxManager
	logger *slog.Logger
	newID  func() uuid.UUID
}

func NewTransferService(tx repository.TxManager, logger *slog.Logger) *TransferService {
	return &TransferService{tx: tx, logger: logger, newID: uuid.New}
}

// CreateTransfer executes a transfer, or returns the stored result for a repeated
// idempotency key.
//
// Everything happens in one transaction (see docs/design.md section 5):
//  1. lock both wallets in id order; a missing wallet aborts with ErrWalletNotFound
//  2. claim the idempotency key by inserting a PENDING transfer; if the key is
//     taken, return the stored transfer (or ErrIdempotencyKeyReused)
//  3. without enough funds, mark the transfer FAILED and commit
//  4. otherwise move the balances, write the ledger pair and mark it PROCESSED
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (Result, error) {
	start := time.Now()
	if err := req.Validate(); err != nil {
		return Result{}, err
	}

	var res Result
	err := s.tx.WithinTx(ctx, func(repo repository.Repository) error {
		var err error
		res, err = s.execute(ctx, repo, req)
		return err
	})
	s.log(ctx, req, res, err, time.Since(start))
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

func (s *TransferService) execute(ctx context.Context, repo repository.Repository, req domain.TransferRequest) (Result, error) {
	wallets, err := repo.LockWallets(ctx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return Result{}, err
	}
	from, ok := findWallet(wallets, req.FromWalletID)
	if !ok {
		return Result{}, fmt.Errorf("source %q: %w", req.FromWalletID, domain.ErrWalletNotFound)
	}
	if _, ok := findWallet(wallets, req.ToWalletID); !ok {
		return Result{}, fmt.Errorf("destination %q: %w", req.ToWalletID, domain.ErrWalletNotFound)
	}

	t := domain.NewTransfer(s.newID(), req)
	inserted, err := repo.InsertTransfer(ctx, &t)
	if err != nil {
		return Result{}, err
	}
	if !inserted {
		return s.replay(ctx, repo, req)
	}

	if !from.CanDebit(t.Amount) {
		if err := t.MarkFailed(domain.FailureInsufficientFunds); err != nil {
			return Result{}, err
		}
		return Result{Transfer: t}, repo.UpdateTransferStatus(ctx, t)
	}

	if err := repo.AddToBalance(ctx, t.FromWalletID, -t.Amount); err != nil {
		return Result{}, err
	}
	if err := repo.AddToBalance(ctx, t.ToWalletID, t.Amount); err != nil {
		return Result{}, err
	}
	if err := t.MarkProcessed(); err != nil {
		return Result{}, err
	}
	if err := repo.InsertLedgerEntries(ctx, t.LedgerEntries()); err != nil {
		return Result{}, err
	}
	return Result{Transfer: t}, repo.UpdateTransferStatus(ctx, t)
}

// replay returns the transfer already stored under the request's idempotency key.
// When InsertTransfer reported the key as taken, the owning transaction has
// committed, so this read (a new statement under READ COMMITTED) sees it.
func (s *TransferService) replay(ctx context.Context, repo repository.Repository, req domain.TransferRequest) (Result, error) {
	existing, err := repo.TransferByIdempotencyKey(ctx, req.IdempotencyKey)
	if err != nil {
		return Result{}, err
	}
	if !existing.MatchesRequest(req) {
		return Result{}, fmt.Errorf("key %q: %w", req.IdempotencyKey, domain.ErrIdempotencyKeyReused)
	}
	return Result{Transfer: existing, Replayed: true}, nil
}

func findWallet(wallets []domain.Wallet, id string) (domain.Wallet, bool) {
	for _, w := range wallets {
		if w.ID == id {
			return w, true
		}
	}
	return domain.Wallet{}, false
}

// log writes one structured line per transfer request.
func (s *TransferService) log(ctx context.Context, req domain.TransferRequest, res Result, err error, elapsed time.Duration) {
	attrs := []any{
		slog.String("idempotency_key", req.IdempotencyKey),
		slog.String("from", req.FromWalletID),
		slog.String("to", req.ToWalletID),
		slog.Int64("amount", req.Amount),
		slog.Int64("duration_ms", elapsed.Milliseconds()),
	}
	switch {
	case err != nil:
		s.logger.WarnContext(ctx, "transfer rejected", append(attrs, slog.String("error", err.Error()))...)
	default:
		s.logger.InfoContext(ctx, "transfer completed", append(attrs,
			slog.String("transfer_id", res.Transfer.ID.String()),
			slog.String("status", string(res.Transfer.Status)),
			slog.Bool("replayed", res.Replayed))...)
	}
}
