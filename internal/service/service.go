// Package service moves money between wallets inside one database transaction.
package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

const idempotencyAttempts = 3

// Service orchestrates wallets and transfers.
type Service struct {
	store repository.Store
}

// New returns a service that uses store for persistence.
func New(store repository.Store) *Service {
	return &Service{store: store}
}

// Request is a wallet-to-wallet transfer.
type Request struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Result is the HTTP status and the exact JSON body to return.
type Result struct {
	Status     int
	Body       []byte
	Replayed   bool
	TransferID string
}

// TransferView is the public transfer document.
type TransferView struct {
	TransferID     string `json:"transferId"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	FailureReason  string `json:"failureReason,omitempty"`
}

// LedgerView is one public ledger row.
type LedgerView struct {
	ID         int64  `json:"entryId"`
	WalletID   string `json:"walletId"`
	TransferID string `json:"transferId"`
	Type       string `json:"type"`
	Amount     int64  `json:"amount"`
}

// TransferDetails is a transfer plus its ledger entries.
type TransferDetails struct {
	Transfer TransferView `json:"transfer"`
	Entries  []LedgerView `json:"entries"`
}

// WalletView is the public wallet document.
type WalletView struct {
	ID             string `json:"id"`
	Balance        int64  `json:"balance"`
	InitialBalance int64  `json:"initialBalance"`
}

// CreateWallet stores a wallet. The balance starts equal to initialBalance.
func (s *Service) CreateWallet(ctx context.Context, id string, initialBalance int64) (WalletView, error) {
	if err := domain.ValidateWalletID(id); err != nil {
		return WalletView{}, err
	}
	if initialBalance < 0 {
		return WalletView{}, &domain.InputError{Message: "initialBalance must be zero or greater"}
	}
	wallet := domain.Wallet{ID: id, Balance: initialBalance, InitialBalance: initialBalance}
	if err := s.store.CreateWallet(ctx, wallet); err != nil {
		return WalletView{}, err
	}
	return toWalletView(wallet), nil
}

// GetWallet returns the current stored balance.
func (s *Service) GetWallet(ctx context.Context, id string) (WalletView, error) {
	wallet, err := s.store.GetWallet(ctx, id)
	if err != nil {
		return WalletView{}, err
	}
	return toWalletView(wallet), nil
}

// GetTransfer returns one transfer and its ledger entries.
func (s *Service) GetTransfer(ctx context.Context, id string) (TransferDetails, error) {
	transfer, entries, err := s.store.GetTransfer(ctx, id)
	if err != nil {
		return TransferDetails{}, err
	}
	views := make([]LedgerView, 0, len(entries))
	for _, entry := range entries {
		views = append(views, LedgerView{
			ID:         entry.ID,
			WalletID:   entry.WalletID,
			TransferID: entry.TransferID,
			Type:       string(entry.Type),
			Amount:     entry.Amount,
		})
	}
	return TransferDetails{Transfer: toTransferView(transfer), Entries: views}, nil
}

// CreateTransfer claims the idempotency key and applies the movement once.
func (s *Service) CreateTransfer(ctx context.Context, req Request) (Result, error) {
	if err := domain.ValidateTransfer(req.FromWalletID, req.ToWalletID, req.IdempotencyKey, req.Amount); err != nil {
		return Result{}, err
	}
	hash := domain.Fingerprint(req.FromWalletID, req.ToWalletID, req.Amount)

	var result Result
	err := s.store.WithTx(ctx, func(tx repository.Tx) error {
		var runErr error
		result, runErr = s.claimAndRun(ctx, tx, req, hash)
		return runErr
	})
	if err != nil {
		return Result{}, err
	}

	slog.InfoContext(ctx, "transfer completed",
		"transfer_id", result.TransferID,
		"idempotency_key", req.IdempotencyKey,
		"from_wallet_id", req.FromWalletID,
		"to_wallet_id", req.ToWalletID,
		"http_status", result.Status,
		"replayed", result.Replayed,
	)
	return result, nil
}

func (s *Service) claimAndRun(ctx context.Context, tx repository.Tx, req Request, hash string) (Result, error) {
	for range idempotencyAttempts {
		_, inserted, err := tx.ClaimIdempotency(ctx, req.IdempotencyKey, hash)
		if err != nil {
			return Result{}, err
		}
		if inserted {
			return s.execute(ctx, tx, req)
		}

		locked, found, err := tx.LockIdempotency(ctx, req.IdempotencyKey)
		if err != nil {
			return Result{}, err
		}
		if !found {
			// The owner rolled back after this attempt conflicted. Claim the key again.
			continue
		}
		if locked.RequestHash != hash {
			return Result{}, domain.ErrIdempotencyConflict
		}
		if locked.ResponseStatus == 0 || len(locked.ResponseBody) == 0 {
			return Result{}, domain.ErrBusy
		}
		return Result{
			Status:     locked.ResponseStatus,
			Body:       locked.ResponseBody,
			Replayed:   true,
			TransferID: locked.TransferID,
		}, nil
	}
	return Result{}, domain.ErrBusy
}

func (s *Service) execute(ctx context.Context, tx repository.Tx, req Request) (Result, error) {
	wallets, err := tx.LockWallets(ctx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return Result{}, err
	}
	from, fromOK := wallets[req.FromWalletID]
	to, toOK := wallets[req.ToWalletID]
	if !fromOK || !toOK {
		// Roll back the idempotency claim so a later retry can succeed after the wallet exists.
		return Result{}, domain.ErrNotFound
	}

	transferID, err := tx.InsertTransfer(ctx, domain.Transfer{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.StatusPending,
	})
	if err != nil {
		return Result{}, err
	}

	if from.Balance < req.Amount {
		return s.fail(ctx, tx, req, transferID, domain.FailureInsufficientFunds)
	}
	if domain.CreditOverflow(to.Balance, req.Amount) {
		return s.fail(ctx, tx, req, transferID, domain.FailureBalanceOverflow)
	}

	if err := tx.InsertLedgerPair(ctx, transferID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
		return Result{}, err
	}
	// The balance check above is an optimization. The conditional debit is the guard.
	// If it fails while this transaction holds the row lock, something else is wrong.
	// Return the error so the whole transaction, including the ledger pair, rolls back.
	if err := tx.DebitWallet(ctx, req.FromWalletID, req.Amount); err != nil {
		return Result{}, err
	}
	if err := tx.CreditWallet(ctx, req.ToWalletID, req.Amount); err != nil {
		return Result{}, err
	}
	if err := tx.UpdateTransferStatus(ctx, transferID, domain.StatusPending, domain.StatusProcessed, ""); err != nil {
		return Result{}, err
	}

	view := TransferView{
		TransferID:     transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         string(domain.StatusProcessed),
	}
	return s.finish(ctx, tx, req.IdempotencyKey, transferID, http.StatusCreated, view)
}

func (s *Service) fail(ctx context.Context, tx repository.Tx, req Request, transferID, reason string) (Result, error) {
	if err := tx.UpdateTransferStatus(ctx, transferID, domain.StatusPending, domain.StatusFailed, reason); err != nil {
		return Result{}, err
	}
	view := TransferView{
		TransferID:     transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         string(domain.StatusFailed),
		FailureReason:  reason,
	}
	return s.finish(ctx, tx, req.IdempotencyKey, transferID, http.StatusUnprocessableEntity, view)
}

func (s *Service) finish(ctx context.Context, tx repository.Tx, key, transferID string, status int, view TransferView) (Result, error) {
	body, err := json.Marshal(view)
	if err != nil {
		return Result{}, err
	}
	if err := tx.SaveIdempotencyResult(ctx, key, status, body, transferID); err != nil {
		return Result{}, err
	}
	return Result{Status: status, Body: body, TransferID: transferID}, nil
}

func toWalletView(wallet domain.Wallet) WalletView {
	return WalletView{ID: wallet.ID, Balance: wallet.Balance, InitialBalance: wallet.InitialBalance}
}

func toTransferView(transfer domain.Transfer) TransferView {
	return TransferView{
		TransferID:     transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         string(transfer.Status),
		FailureReason:  transfer.FailureReason,
	}
}
