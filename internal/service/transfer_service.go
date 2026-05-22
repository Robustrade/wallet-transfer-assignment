package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/domain"
	"github.com/HarshilJariwala2000/wallet-transfer-assignment/internal/repository"
)

// CreateTransferCommand is the input to a transfer request.
type CreateTransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// TransferResponse is the API-facing result of a transfer.
type TransferResponse struct {
	ID           string `json:"id"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
}

// Result wraps the response and whether it was replayed from a stored
// idempotency record.
type Result struct {
	Response TransferResponse
	Replayed bool
}

// TransferService holds the transfer workflow: validation, idempotency
// behavior, lock ordering, double-entry orchestration, and state transitions.
// It owns the transaction boundary via the store's WithTx; the store only
// performs persistence.
type TransferService struct {
	store *repository.Store
}

func NewTransferService(store *repository.Store) *TransferService {
	return &TransferService{store: store}
}

// CreateTransfer executes a wallet-to-wallet transfer with exactly-once
// semantics for the idempotency key. The entire workflow lives in this single
// method so the behavior can be read top-to-bottom; it runs as one atomic
// transaction via store.WithTx.
func (s *TransferService) CreateTransfer(ctx context.Context, cmd CreateTransferCommand) (*Result, error) {
	if cmd.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}
	if cmd.FromWalletID == cmd.ToWalletID {
		return nil, domain.ErrSameWallet
	}
	reqHash := hashRequest(cmd)

	var result Result
	err := s.store.WithTx(ctx, func(q *repository.Queries) error {
		// 1. Idempotency: replay an existing result or reject key reuse.
		if rec, err := q.GetIdempotency(ctx, cmd.IdempotencyKey); err != nil {
			return err
		} else if rec != nil {
			if rec.RequestHash != reqHash {
				return domain.ErrIdempotencyConflict
			}
			t, err := q.GetTransfer(ctx, rec.TransferID)
			if err != nil {
				return err
			}
			result = Result{Response: toResponse(t), Replayed: true}
			return nil
		}

		// 2. Lock both wallets in deterministic order to prevent deadlocks.
		from, to, err := lockWalletPair(ctx, q, cmd.FromWalletID, cmd.ToWalletID)
		if err != nil {
			return err
		}

		// 3. Record the transfer in PENDING before any money movement.
		transfer := domain.Transfer{
			ID:           uuid.NewString(),
			FromWalletID: cmd.FromWalletID,
			ToWalletID:   cmd.ToWalletID,
			Amount:       cmd.Amount,
			Status:       domain.StatusPending,
		}
		if err := q.InsertTransfer(ctx, transfer); err != nil {
			return err
		}

		// 4. Decide the terminal state. Insufficient funds is a recorded
		//    outcome (PENDING -> FAILED) that commits with balances unchanged.
		terminal := domain.StatusProcessed
		if !from.CanDebit(cmd.Amount) {
			terminal = domain.StatusFailed
		}

		if terminal == domain.StatusProcessed {
			// 5. Double-entry ledger: exactly one DEBIT and one CREDIT.
			if err := q.InsertLedgerEntry(ctx, ledgerEntry(transfer, from.ID, domain.Debit)); err != nil {
				return err
			}
			if err := q.InsertLedgerEntry(ctx, ledgerEntry(transfer, to.ID, domain.Credit)); err != nil {
				return err
			}
			// 6. Update both stored balances in the same transaction.
			if err := q.UpdateWalletBalance(ctx, from.ID, from.Balance-cmd.Amount); err != nil {
				return err
			}
			if err := q.UpdateWalletBalance(ctx, to.ID, to.Balance+cmd.Amount); err != nil {
				return err
			}
		}

		// 7. Apply the state transition, guarded by the domain state machine.
		if !transfer.Status.CanTransitionTo(terminal) {
			return domain.ErrInvalidTransition
		}
		if err := q.UpdateTransferStatus(ctx, transfer.ID, terminal); err != nil {
			return err
		}
		transfer.Status = terminal

		// 8. Persist the idempotency record. A concurrent request with the
		//    same key fails here on the PRIMARY KEY; we surface that so the
		//    transaction rolls back and the result is replayed.
		if err := q.InsertIdempotency(ctx, cmd.IdempotencyKey, transfer.ID, reqHash); err != nil {
			return err
		}

		result = Result{Response: toResponse(transfer)}
		return nil
	})

	// Lost the idempotency-key race: the winner committed, so replay its result.
	if errors.Is(err, repository.ErrIdempotencyKeyExists) {
		return s.replay(ctx, cmd.IdempotencyKey, reqHash)
	}
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// replay reads the result committed by the request that won the
// idempotency-key race.
func (s *TransferService) replay(ctx context.Context, key, reqHash string) (*Result, error) {
	var result Result
	err := s.store.WithTx(ctx, func(q *repository.Queries) error {
		rec, err := q.GetIdempotency(ctx, key)
		if err != nil {
			return err
		}
		if rec == nil {
			return errors.New("idempotency record missing after unique violation")
		}
		if rec.RequestHash != reqHash {
			return domain.ErrIdempotencyConflict
		}
		t, err := q.GetTransfer(ctx, rec.TransferID)
		if err != nil {
			return err
		}
		result = Result{Response: toResponse(t), Replayed: true}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// lockWalletPair locks both wallets ordered by ID, then maps them back to
// source and destination.
func lockWalletPair(ctx context.Context, q *repository.Queries, fromID, toID string) (from, to domain.Wallet, err error) {
	firstID, secondID := fromID, toID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	w1, err := q.GetWalletForUpdate(ctx, firstID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	w2, err := q.GetWalletForUpdate(ctx, secondID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	locked := map[string]domain.Wallet{w1.ID: w1, w2.ID: w2}
	return locked[fromID], locked[toID], nil
}

func ledgerEntry(t domain.Transfer, walletID string, typ domain.EntryType) domain.LedgerEntry {
	return domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: t.ID,
		WalletID:   walletID,
		Type:       typ,
		Amount:     t.Amount,
	}
}

func toResponse(t domain.Transfer) TransferResponse {
	return TransferResponse{
		ID:           t.ID,
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount,
		Status:       string(t.Status),
	}
}

// hashRequest fingerprints the meaningful request parameters so reusing an
// idempotency key with different parameters can be detected.
func hashRequest(cmd CreateTransferCommand) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%s|%d", cmd.FromWalletID, cmd.ToWalletID, cmd.Amount))
	return hex.EncodeToString(sum[:])
}
