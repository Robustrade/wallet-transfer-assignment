package main

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		// Fall back to a deterministic byte pattern only if entropy is unavailable.
		for i := range b {
			b[i] = byte(i + 1)
		}
	}
	return hex.EncodeToString(b)
}

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResult struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
}

func generateID() string {
	return fmt.Sprintf("t_%d_%s", time.Now().UnixNano(), randomHex(8))
}

// Transfer implements a transactional, idempotent wallet transfer.
func (s *Service) Transfer(ctx context.Context, req TransferRequest) (TransferResult, error) {
	if req.Amount <= 0 {
		return TransferResult{}, errors.New("amount must be > 0")
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return TransferResult{}, errors.New("fromWalletId and toWalletId are required")
	}
	if req.FromWalletID == req.ToWalletID {
		return TransferResult{}, errors.New("fromWalletId and toWalletId must be different")
	}
	// optimistic claim via idempotency record
	// try to insert idempotency record; if exists and completed return existing transfer
	// if exists and in-progress, poll for completion

	// Try quick path: check if completed.
	// Validate that a completed idempotency record includes a transfer_id
	// before returning. The repository stores `status` and `response`, so
	// prefer the stored response as the canonical terminal state when present.
	if req.IdempotencyKey != "" {
		tid, status, resp, err := s.repo.GetIdempotency(ctx, req.IdempotencyKey)
		if err == nil {
			if status == "COMPLETED" {
				if tid == "" {
					return TransferResult{}, errors.New("idempotency record completed but missing transfer id")
				}
				state := "PROCESSED"
				if resp != "" {
					state = resp
				}
				return TransferResult{TransferID: tid, State: state}, nil
			}
			if status == "FAILED" {
				// return stored error when possible
				if resp == "insufficient_funds" {
					return TransferResult{}, ErrInsufficientFunds
				}
				return TransferResult{}, errors.New(resp)
			}
		}
	}

	// insert a claim; use a DB transaction so that competing clients will fail the insert
	tx, err := s.repo.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return TransferResult{}, err
	}
	defer tx.Rollback()

	if req.IdempotencyKey != "" {
		if err := s.repo.InsertIdempotency(ctx, tx, req.IdempotencyKey, "IN_PROGRESS"); err != nil {
			// insertion failed: someone else claimed it. release tx and poll for completion with timeout
			tx.Rollback()
			wait := time.Millisecond * 50
			pollCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			ticker := time.NewTicker(wait)
			defer ticker.Stop()
			for {
				select {
				case <-pollCtx.Done():
					return TransferResult{}, errors.New("idempotency key in progress (timeout)")
				case <-ticker.C:
					tid, status, resp, err := s.repo.GetIdempotency(pollCtx, req.IdempotencyKey)
					if err == nil {
						if status == "COMPLETED" {
							if tid == "" {
								return TransferResult{}, errors.New("idempotency record completed but missing transfer id")
							}
							state := "PROCESSED"
							if resp != "" {
								state = resp
							}
							return TransferResult{TransferID: tid, State: state}, nil
						}
						if status == "FAILED" {
							if resp == "insufficient_funds" {
								return TransferResult{}, ErrInsufficientFunds
							}
							return TransferResult{}, errors.New(resp)
						}
					}
				}
			}
		}
	}

	transferID := generateID()
	t := Transfer{ID: transferID, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount, State: "PENDING"}
	if err := s.repo.InsertTransfer(ctx, tx, t); err != nil {
		return TransferResult{}, err
	}

	// try to debit
	if err := s.repo.DebitIfEnough(ctx, tx, req.FromWalletID, req.Amount); err != nil {
		if errors.Is(err, ErrInsufficientFunds) {
			if uerr := s.repo.UpdateTransferState(ctx, tx, transferID, "FAILED"); uerr != nil {
				tx.Rollback()
				return TransferResult{}, uerr
			}
			if req.IdempotencyKey != "" {
				if cerr := s.repo.CompleteIdempotency(ctx, tx, req.IdempotencyKey, transferID, "FAILED", "insufficient_funds"); cerr != nil {
					tx.Rollback()
					return TransferResult{}, cerr
				}
			}
			if cerr := tx.Commit(); cerr != nil {
				return TransferResult{}, cerr
			}
			return TransferResult{}, ErrInsufficientFunds
		}
		return TransferResult{}, err
	}

	if err := s.repo.Credit(ctx, tx, req.ToWalletID, req.Amount); err != nil {
		if uerr := s.repo.UpdateTransferState(ctx, tx, transferID, "FAILED"); uerr != nil {
			tx.Rollback()
			return TransferResult{}, uerr
		}
		if req.IdempotencyKey != "" {
			if cerr := s.repo.CompleteIdempotency(ctx, tx, req.IdempotencyKey, transferID, "FAILED", err.Error()); cerr != nil {
				tx.Rollback()
				return TransferResult{}, cerr
			}
		}
		return TransferResult{}, err
	}

	// ledger entries
	debit := LedgerEntry{WalletID: req.FromWalletID, TransferID: transferID, Type: "DEBIT", Amount: req.Amount}
	credit := LedgerEntry{WalletID: req.ToWalletID, TransferID: transferID, Type: "CREDIT", Amount: req.Amount}
	if err := s.repo.InsertLedgerEntry(ctx, tx, debit); err != nil {
		if uerr := s.repo.UpdateTransferState(ctx, tx, transferID, "FAILED"); uerr != nil {
			tx.Rollback()
			return TransferResult{}, uerr
		}
		if req.IdempotencyKey != "" {
			if cerr := s.repo.CompleteIdempotency(ctx, tx, req.IdempotencyKey, transferID, "FAILED", err.Error()); cerr != nil {
				tx.Rollback()
				return TransferResult{}, cerr
			}
		}
		return TransferResult{}, err
	}
	if err := s.repo.InsertLedgerEntry(ctx, tx, credit); err != nil {
		if uerr := s.repo.UpdateTransferState(ctx, tx, transferID, "FAILED"); uerr != nil {
			tx.Rollback()
			return TransferResult{}, uerr
		}
		if req.IdempotencyKey != "" {
			if cerr := s.repo.CompleteIdempotency(ctx, tx, req.IdempotencyKey, transferID, "FAILED", err.Error()); cerr != nil {
				tx.Rollback()
				return TransferResult{}, cerr
			}
		}
		return TransferResult{}, err
	}

	if err := s.repo.UpdateTransferState(ctx, tx, transferID, "PROCESSED"); err != nil {
		tx.Rollback()
		return TransferResult{}, err
	}

	if req.IdempotencyKey != "" {
		if err := s.repo.CompleteIdempotency(ctx, tx, req.IdempotencyKey, transferID, "COMPLETED", "PROCESSED"); err != nil {
			tx.Rollback()
			return TransferResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return TransferResult{}, err
	}

	return TransferResult{TransferID: transferID, State: "PROCESSED"}, nil
}
