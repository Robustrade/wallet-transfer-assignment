package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferService struct {
	db           *pgxpool.Pool
	walletRepo   repository.WalletRepository
	transferRepo repository.TransferRepository
	ledgerRepo   repository.LedgerRepository
}

var (
	ErrInvalidRequest      = errors.New("invalid transfer request")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInsufficientBalance = errors.New("insufficient balance")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
)

func NewTransferService(
	db *pgxpool.Pool,
	walletRepo repository.WalletRepository,
	transferRepo repository.TransferRepository,
	ledgerRepo repository.LedgerRepository,
) *TransferService {
	return &TransferService{
		db:           db,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		ledgerRepo:   ledgerRepo,
	}
}

func validateTransferRequest(req CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency key is required", ErrInvalidRequest)
	}

	if req.FromWalletID == "" {
		return fmt.Errorf("%w: from wallet ID is required", ErrInvalidRequest)
	}

	if req.ToWalletID == "" {
		return fmt.Errorf("%w: to wallet ID is required", ErrInvalidRequest)
	}

	if req.FromWalletID == req.ToWalletID {
		return fmt.Errorf("%w: source and destination wallets must be different", ErrInvalidRequest)
	}

	if req.Amount <= 0 {
		return fmt.Errorf(
			"%w: amount must be greater than zero",
			ErrInvalidRequest,
		)
	}

	return nil
}

func requestHash(req CreateTransferRequest) string {
	payload := fmt.Sprintf(
		"%s:%s:%d",
		req.FromWalletID,
		req.ToWalletID,
		req.Amount,
	)

	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func orderedWalletIDs(fromID, toID string) []string {
	ids := []string{fromID, toID}
	sort.Strings(ids)
	return ids
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req CreateTransferRequest,
) (*domain.Transfer, error) {
	if err := validateTransferRequest(req); err != nil {
		return nil, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	hash := requestHash(req)

	// Check whether this idempotency key has already been used.
	existing, err := s.transferRepo.GetByIdempotencyKey(
		ctx,
		tx,
		req.IdempotencyKey,
	)
	if err != nil {
		return nil, err
	}

	if existing != nil {
		if existing.RequestHash != hash {
			return nil, ErrIdempotencyConflict
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf(
				"commit idempotent response: %w",
				err,
			)
		}

		if existing.Status == domain.TransferFailed {
			return existing, ErrInsufficientBalance
		}

		return existing, nil
	}

	transfer := &domain.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.TransferPending,
		RequestHash:    hash,
	}

	// Lock both wallets in deterministic order.
	orderedIDs := orderedWalletIDs(
		req.FromWalletID,
		req.ToWalletID,
	)

	wallets := make(map[string]*domain.Wallet, 2)

	for _, walletID := range orderedIDs {
		wallet, err := s.walletRepo.GetByIDForUpdate(
			ctx,
			tx,
			walletID,
		)
		if err != nil {
			if errors.Is(err, repository.ErrWalletNotFound) {
				return nil, fmt.Errorf(
					"%w: %s",
					ErrWalletNotFound,
					walletID,
				)
			}

			return nil, err
		}

		wallets[walletID] = wallet
	}

	if err := s.transferRepo.Create(ctx, tx, transfer); err != nil {
		// Another concurrent request may have created the same
		// idempotency key between our SELECT and INSERT.
		if isUniqueViolation(err) {
			_ = tx.Rollback(ctx)

			return s.getExistingTransfer(
				ctx,
				req.IdempotencyKey,
				hash,
			)
		}

		return nil, err
	}

	fromWallet := wallets[req.FromWalletID]
	toWallet := wallets[req.ToWalletID]

	if fromWallet.Balance < req.Amount {
		if err := s.transferRepo.UpdateStatus(
			ctx,
			tx,
			transfer.ID,
			domain.TransferFailed,
		); err != nil {
			return nil, err
		}

		transfer.Status = domain.TransferFailed

		persistedTransfer, err := s.transferRepo.GetByIdempotencyKey(
			ctx,
			tx,
			req.IdempotencyKey,
		)
		if err != nil {
			return nil, err
		}

		if persistedTransfer == nil {
			return nil, fmt.Errorf("transfer disappeared after status update")
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit failed transfer: %w", err)
		}

		return persistedTransfer, ErrInsufficientBalance
	}

	if err := s.walletRepo.UpdateBalance(
		ctx,
		tx,
		fromWallet.ID,
		fromWallet.Balance-req.Amount,
	); err != nil {
		return nil, err
	}

	if err := s.walletRepo.UpdateBalance(
		ctx,
		tx,
		toWallet.ID,
		toWallet.Balance+req.Amount,
	); err != nil {
		return nil, err
	}

	debit := &domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transfer.ID,
		WalletID:   fromWallet.ID,
		Type:       domain.LedgerDebit,
		Amount:     req.Amount,
	}

	if err := s.ledgerRepo.Create(ctx, tx, debit); err != nil {
		return nil, err
	}

	credit := &domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transfer.ID,
		WalletID:   toWallet.ID,
		Type:       domain.LedgerCredit,
		Amount:     req.Amount,
	}

	if err := s.ledgerRepo.Create(ctx, tx, credit); err != nil {
		return nil, err
	}

	if err := s.transferRepo.UpdateStatus(
		ctx,
		tx,
		transfer.ID,
		domain.TransferProcessed,
	); err != nil {
		return nil, err
	}

	transfer.Status = domain.TransferProcessed

	persistedTransfer, err := s.transferRepo.GetByIdempotencyKey(
		ctx,
		tx,
		req.IdempotencyKey,
	)
	if err != nil {
		return nil, err
	}

	if persistedTransfer == nil {
		return nil, fmt.Errorf("transfer disappeared after status update")
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transfer: %w", err)
	}

	return persistedTransfer, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError

	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505"
}

func (s *TransferService) getExistingTransfer(
	ctx context.Context,
	idempotencyKey string,
	expectedHash string,
) (*domain.Transfer, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin lookup transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	transfer, err := s.transferRepo.GetByIdempotencyKey(
		ctx,
		tx,
		idempotencyKey,
	)
	if err != nil {
		return nil, err
	}

	if transfer == nil {
		return nil, fmt.Errorf(
			"transfer not found after idempotency conflict",
		)
	}

	if transfer.RequestHash != expectedHash {
		return nil, fmt.Errorf(
			"%w: idempotency key already used with a different request",
			ErrIdempotencyConflict,
		)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf(
			"commit lookup transaction: %w",
			err,
		)
	}

	if transfer.Status == domain.TransferFailed {
		return transfer, ErrInsufficientBalance
	}

	return transfer, nil
}
