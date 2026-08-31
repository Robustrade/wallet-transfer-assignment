package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/repository"
)

// ============================================================
// Errors
// ============================================================

var (
	ErrInsufficientBalance = errors.New("insufficient wallet balance")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrIdempotencyConflict = errors.New("idempotency key already used with different request")
)

// ============================================================
// Input
// ============================================================

type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// ============================================================
// Response
// ============================================================

type TransferResponse struct {
	TransferID   string `json:"transferId"`
	Status       string `json:"status"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}

// ============================================================
// Service
// ============================================================

type TransferService struct {
	repo repository.TransferRepository
}

// ============================================================
// Constructor
// ============================================================

func NewTransferService(
	repo repository.TransferRepository,
) *TransferService {

	return &TransferService{
		repo: repo,
	}
}

// ============================================================
// Create Transfer
// ============================================================

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	input CreateTransferInput,
) (*TransferResponse, error) {

	// --------------------------------------------------------
	// Basic validation
	// --------------------------------------------------------

	if input.FromWalletID == input.ToWalletID {
		return nil, errors.New(
			"source and destination wallets must be different",
		)
	}

	if input.Amount <= 0 {
		return nil, errors.New("amount must be greater than zero")
	}

	// --------------------------------------------------------
	// Start database transaction
	// --------------------------------------------------------

	// We need access to the PostgreSQL transaction.
	//
	// Since the repository interface receives pgx.Tx,
	// the repository needs to expose the DB transaction.
	//
	// We'll handle this through a transaction-capable
	// repository in the next section.
	//
	// For this version, the repository provides BeginTx.
	// --------------------------------------------------------

	txRepo, ok := s.repo.(interface {
		BeginTx(
			ctx context.Context,
		) (pgx.Tx, error)
	})

	if !ok {
		return nil, errors.New(
			"repository does not support transactions",
		)
	}

	tx, err := txRepo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}

	defer tx.Rollback(ctx)

	// --------------------------------------------------------
	// 1. Idempotency check
	// --------------------------------------------------------

	existing, err := s.repo.GetTransferByIdempotencyKey(
		ctx,
		tx,
		input.IdempotencyKey,
	)

	if err == nil {

		// Same idempotency key already exists.
		// Return original response.

		if existing.Response != nil {
			var response TransferResponse

			if err := json.Unmarshal(
				existing.Response,
				&response,
			); err == nil {

				// Check that the same request is being retried.
				if existing.FromWalletID != input.FromWalletID ||
					existing.ToWalletID != input.ToWalletID ||
					existing.Amount != input.Amount {

					return nil, ErrIdempotencyConflict
				}

				return &response, nil
			}
		}

		// If response is not available, return
		// the existing transfer state.
		return transferToResponse(existing), nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// --------------------------------------------------------
	// 2. Create PENDING transfer
	// --------------------------------------------------------

	transferID := uuid.New().String()

	transfer := &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: input.IdempotencyKey,
		FromWalletID:   input.FromWalletID,
		ToWalletID:     input.ToWalletID,
		Amount:         input.Amount,
		Status:         domain.StatusPending,
	}

	err = s.repo.CreateTransfer(
		ctx,
		tx,
		transfer,
	)

	if err != nil {

		// Another concurrent request may have inserted
		// the same idempotency key.

		existing, getErr :=
			s.repo.GetTransferByIdempotencyKey(
				ctx,
				tx,
				input.IdempotencyKey,
			)

		if getErr != nil {
			return nil, err
		}

		if existing.FromWalletID != input.FromWalletID ||
			existing.ToWalletID != input.ToWalletID ||
			existing.Amount != input.Amount {

			return nil, ErrIdempotencyConflict
		}

		return transferToResponse(existing), nil
	}

	// --------------------------------------------------------
	// 3. Lock both wallets
	// --------------------------------------------------------

	walletIDs := []string{
		input.FromWalletID,
		input.ToWalletID,
	}

	// Deterministic locking order prevents deadlocks.
	sort.Strings(walletIDs)

	wallet1, err := s.repo.LockWallet(
		ctx,
		tx,
		walletIDs[0],
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}

		return nil, err
	}

	wallet2, err := s.repo.LockWallet(
		ctx,
		tx,
		walletIDs[1],
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrWalletNotFound
		}

		return nil, err
	}

	// --------------------------------------------------------
	// Identify source and destination
	// --------------------------------------------------------

	var sourceWallet *domain.Wallet
	var destinationWallet *domain.Wallet

	if wallet1.ID == input.FromWalletID {
		sourceWallet = wallet1
		destinationWallet = wallet2
	} else {
		sourceWallet = wallet2
		destinationWallet = wallet1
	}

	// --------------------------------------------------------
	// 4. Check balance
	// --------------------------------------------------------

	if sourceWallet.Balance < input.Amount {

		response := &TransferResponse{
			TransferID:   transferID,
			Status:       string(domain.StatusFailed),
			FromWalletID: input.FromWalletID,
			ToWalletID:   input.ToWalletID,
			Amount:       input.Amount,
		}

		responseJSON, err := json.Marshal(response)
		if err != nil {
			return nil, err
		}

		err = s.repo.UpdateTransferStatus(
			ctx,
			tx,
			transferID,
			domain.StatusFailed,
			responseJSON,
		)

		if err != nil {
			return nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}

		return response, nil
	}

	// --------------------------------------------------------
	// 5. Debit source wallet
	// --------------------------------------------------------

	newSourceBalance :=
		sourceWallet.Balance - input.Amount

	err = s.repo.UpdateWalletBalance(
		ctx,
		tx,
		sourceWallet.ID,
		newSourceBalance,
	)

	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// 6. Credit destination wallet
	// --------------------------------------------------------

	newDestinationBalance :=
		destinationWallet.Balance + input.Amount

	err = s.repo.UpdateWalletBalance(
		ctx,
		tx,
		destinationWallet.ID,
		newDestinationBalance,
	)

	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// 7. Create DEBIT ledger entry
	// --------------------------------------------------------

	debitEntry := &domain.LedgerEntry{
		WalletID:   sourceWallet.ID,
		TransferID: transferID,
		Type:       domain.LedgerDebit,
		Amount:     input.Amount,
	}

	err = s.repo.CreateLedgerEntry(
		ctx,
		tx,
		debitEntry,
	)

	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// 8. Create CREDIT ledger entry
	// --------------------------------------------------------

	creditEntry := &domain.LedgerEntry{
		WalletID:   destinationWallet.ID,
		TransferID: transferID,
		Type:       domain.LedgerCredit,
		Amount:     input.Amount,
	}

	err = s.repo.CreateLedgerEntry(
		ctx,
		tx,
		creditEntry,
	)

	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// 9. Create response
	// --------------------------------------------------------

	response := &TransferResponse{
		TransferID:   transferID,
		Status:       string(domain.StatusProcessed),
		FromWalletID: input.FromWalletID,
		ToWalletID:   input.ToWalletID,
		Amount:       input.Amount,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// 10. Mark transfer PROCESSED
	// --------------------------------------------------------

	err = s.repo.UpdateTransferStatus(
		ctx,
		tx,
		transferID,
		domain.StatusProcessed,
		responseJSON,
	)

	if err != nil {
		return nil, err
	}

	// --------------------------------------------------------
	// 11. Commit
	// --------------------------------------------------------

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return response, nil
}

// ============================================================
// Helper
// ============================================================

func transferToResponse(
	t *domain.Transfer,
) *TransferResponse {

	return &TransferResponse{
		TransferID:   t.ID,
		Status:       string(t.Status),
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount,
	}
}
