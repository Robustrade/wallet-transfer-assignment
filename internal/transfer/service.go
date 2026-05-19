package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/hariharandr/wallet-transfer-assignment/internal/apperr"
	"github.com/hariharandr/wallet-transfer-assignment/internal/domain"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

type Request struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type Result struct {
	TransferID string
	State      domain.TransferState
	From       string
	To         string
	Amount     int64
}

// what we stash in the idempotency row so a retry gets the same answer.
// Error is empty for success, set for the known failures.
type storedResult struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
	From       string `json:"from"`
	To         string `json:"to"`
	Amount     int64  `json:"amount"`
	Error      string `json:"error,omitempty"`
}

func (r Request) validate() error {
	if r.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency key is required", apperr.ErrInvalidRequest)
	}
	if r.FromWalletID == "" || r.ToWalletID == "" {
		return fmt.Errorf("%w: wallet ids are required", apperr.ErrInvalidRequest)
	}
	if r.FromWalletID == r.ToWalletID {
		return apperr.ErrSameWallet
	}
	return domain.ValidateAmount(r.Amount)
}

// Fingerprint is a stable hash of the money fields, used to catch the
// same key sent with a different payload.
func Fingerprint(from, to string, amount int64) string {
	h := sha256.Sum256([]byte(from + "|" + to + "|" + strconv.FormatInt(amount, 10)))
	return hex.EncodeToString(h[:])
}

func (s *Service) Transfer(ctx context.Context, req Request) (Result, error) {
	if err := req.validate(); err != nil {
		return Result{}, err
	}

	fp := Fingerprint(req.FromWalletID, req.ToWalletID, req.Amount)

	inserted, err := s.repo.InsertIdempotencyPending(ctx, req.IdempotencyKey, fp)
	if err != nil {
		return Result{}, err
	}
	if !inserted {
		return s.replay(ctx, req, fp)
	}

	var res Result
	var bizErr error
	txErr := s.repo.WithinTx(ctx, func(tx Tx) error {
		ws, e := tx.LockWallets(ctx, req.FromWalletID, req.ToWalletID)
		if e != nil {
			return e
		}

		id, e := tx.InsertTransfer(ctx, domain.Transfer{
			FromWallet: req.FromWalletID,
			ToWallet:   req.ToWalletID,
			Amount:     req.Amount,
			State:      domain.StatePending,
		})
		if e != nil {
			return e
		}

		// not enough money: record a FAILED transfer and a replayable
		// 422, but still commit so the failure is durable.
		if ws[req.FromWalletID].Balance < req.Amount {
			if e = tx.UpdateTransferState(ctx, id, domain.StateFailed, "insufficient funds"); e != nil {
				return e
			}
			body, e := json.Marshal(storedResult{
				TransferID: id, State: domain.StateFailed.String(),
				From: req.FromWalletID, To: req.ToWalletID, Amount: req.Amount,
				Error: "insufficient_funds",
			})
			if e != nil {
				return e
			}
			if e = tx.CompleteIdempotency(ctx, req.IdempotencyKey, id, 422, body); e != nil {
				return e
			}
			res = Result{TransferID: id, State: domain.StateFailed,
				From: req.FromWalletID, To: req.ToWalletID, Amount: req.Amount}
			bizErr = apperr.ErrInsufficientFunds
			return nil
		}

		for _, le := range []domain.LedgerEntry{
			{TransferID: id, WalletID: req.FromWalletID, Type: domain.Debit, Amount: req.Amount},
			{TransferID: id, WalletID: req.ToWalletID, Type: domain.Credit, Amount: req.Amount},
		} {
			if e = tx.InsertLedgerEntry(ctx, le); e != nil {
				return e
			}
		}
		if e = tx.AdjustBalance(ctx, req.FromWalletID, -req.Amount); e != nil {
			return e
		}
		if e = tx.AdjustBalance(ctx, req.ToWalletID, req.Amount); e != nil {
			return e
		}
		if e = tx.UpdateTransferState(ctx, id, domain.StateProcessed, ""); e != nil {
			return e
		}

		res = Result{TransferID: id, State: domain.StateProcessed,
			From: req.FromWalletID, To: req.ToWalletID, Amount: req.Amount}
		body, e := json.Marshal(storedResult{
			TransferID: id, State: domain.StateProcessed.String(),
			From: req.FromWalletID, To: req.ToWalletID, Amount: req.Amount,
		})
		if e != nil {
			return e
		}
		return tx.CompleteIdempotency(ctx, req.IdempotencyKey, id, 201, body)
	})

	if txErr != nil {
		// the only rolled-back business failure is an unknown wallet.
		// close the key so a retry replays the 404 instead of hanging.
		if errors.Is(txErr, apperr.ErrWalletNotFound) {
			body, _ := json.Marshal(storedResult{Error: "wallet_not_found"})
			if e := s.repo.FinalizeIdempotency(ctx, req.IdempotencyKey, 404, body); e != nil {
				return Result{}, e
			}
			return Result{}, apperr.ErrWalletNotFound
		}
		return Result{}, txErr
	}
	if bizErr != nil {
		return res, bizErr
	}
	return res, nil
}

func (s *Service) replay(ctx context.Context, req Request, fp string) (Result, error) {
	rec, err := s.repo.LoadIdempotency(ctx, req.IdempotencyKey)
	if err != nil {
		return Result{}, err
	}
	if rec.Fingerprint != fp {
		return Result{}, apperr.ErrKeyReused
	}
	if rec.Status != "COMPLETED" {
		return Result{}, apperr.ErrInProgress
	}

	var sr storedResult
	if err := json.Unmarshal(rec.ResponseBody, &sr); err != nil {
		return Result{}, fmt.Errorf("decode stored result: %w", err)
	}

	switch sr.Error {
	case "wallet_not_found":
		return Result{}, apperr.ErrWalletNotFound
	case "insufficient_funds":
		var st domain.TransferState
		if err := st.Scan(sr.State); err != nil {
			return Result{}, err
		}
		return Result{TransferID: sr.TransferID, State: st,
			From: sr.From, To: sr.To, Amount: sr.Amount}, apperr.ErrInsufficientFunds
	case "":
		var st domain.TransferState
		if err := st.Scan(sr.State); err != nil {
			return Result{}, err
		}
		return Result{TransferID: sr.TransferID, State: st,
			From: sr.From, To: sr.To, Amount: sr.Amount}, nil
	default:
		return Result{}, fmt.Errorf("unknown stored error %q", sr.Error)
	}
}
