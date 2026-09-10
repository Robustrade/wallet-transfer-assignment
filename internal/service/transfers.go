package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/database"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/helper"
	"github.com/sushmitaw/wallet-transfer-assignment/internal/repository"
)

type TransferService struct {
	db   *database.DB
	repo repository.Repository
}

func NewTransferService(db *database.DB, repo repository.Repository) *TransferService {
	return &TransferService{
		db:   db,
		repo: repo,
	}
}

func (s *TransferService) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (*domain.CreateTransferResult, error) {

	// Step 1: Validate request before starting a transaction.
	if err := helper.ValidateTransferRequest(req); err != nil {
		return nil, err
	}

	requestHash := helper.RequestHash(req)

	// Step 2: Start one transaction for the complete transfer workflow.
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer tx.Rollback()

	// Step 3: Claim idempotency key or replay an existing request.
	idempotencyRecord, replayResult, err := s.claimIdempotencyKey(ctx, tx, req, requestHash)

	if replayResult != nil || err != nil {
		return replayResult, err
	}

	// Step 4: Lock wallets in deterministic order.
	sourceWallet, destinationWallet, err := s.lockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return nil, err
	}

	// Step 5: Create transfer in PENDING state.
	transfer, err := s.createPendingTransfer(ctx, tx, req)
	if err != nil {
		return nil, err
	}

	// Step 6: Business validation.
	if sourceWallet.Balance < req.Amount {
		return s.failInsufficientFunds(ctx, tx, transfer, idempotencyRecord)
	}

	// Step 7: Update wallet balances.
	err = s.updateWalletBalances(ctx, tx, sourceWallet, destinationWallet, req.Amount)
	if err != nil {
		return nil, err
	}

	// Step 8: Create debit and credit ledger entries.
	err = s.createLedgerEntries(ctx, tx, transfer, sourceWallet.ID, destinationWallet.ID)
	if err != nil {
		return nil, err
	}

	// Step 9: Mark transfer processed and persist replay response.
	result, err := s.completeTransfer(ctx, tx, transfer, idempotencyRecord)
	if err != nil {
		return nil, err
	}

	// Step 10: Commit entire transaction.
	err = tx.Commit()
	if err != nil {
		return nil, fmt.Errorf("commit transfer transaction: %w", err)
	}

	return result, nil
}

func (s *TransferService) claimIdempotencyKey(ctx context.Context, tx *sql.Tx, req domain.CreateTransferRequest, requestHash string) (*domain.IdempotencyRecord, *domain.CreateTransferResult, error) {

	record := &domain.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    requestHash,
		CreatedAt:      time.Now().UTC(),
	}

	err := s.repo.CreateIdempotencyRecord(ctx, tx, record)
	if err == nil {
		return record, nil, nil
	}

	if errors.Is(err, domain.ErrIdempotencyKeyExists) {
		result, err := s.handleExistingIdempotencyRecord(ctx, tx, req.IdempotencyKey, requestHash)
		return nil, result, err
	}

	return nil, nil, fmt.Errorf("claim idempotency key: %w", err)
}

func (s *TransferService) createPendingTransfer(ctx context.Context, tx *sql.Tx, req domain.CreateTransferRequest) (*domain.Transfer, error) {

	transfer := &domain.Transfer{
		ID:           uuid.New(),
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       domain.TransferStatusPending,
	}

	if err := s.repo.CreateTransfer(ctx, tx, transfer); err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	return transfer, nil
}

func (s *TransferService) updateWalletBalances(ctx context.Context, tx *sql.Tx, source *domain.Wallet, destination *domain.Wallet, amount int64) error {

	if err := s.repo.UpdateWalletBalance(ctx, tx, source.ID, source.Balance-amount); err != nil {
		return fmt.Errorf("update source wallet balance: %w", err)
	}

	if err := s.repo.UpdateWalletBalance(ctx, tx, destination.ID, destination.Balance+amount); err != nil {
		return fmt.Errorf("update destination wallet balance: %w", err)
	}

	return nil
}

func (s *TransferService) createLedgerEntries(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer, sourceWalletID uuid.UUID, destinationWalletID uuid.UUID) error {

	debit := &domain.LedgerEntry{
		ID:         uuid.New(),
		TransferID: transfer.ID,
		WalletID:   sourceWalletID,
		EntryType:  domain.LedgerEntryTypeDebit,
		Amount:     transfer.Amount,
	}

	if err := s.repo.CreateLedgerEntry(ctx, tx, debit); err != nil {
		return fmt.Errorf("create debit ledger entry: %w", err)
	}

	credit := &domain.LedgerEntry{
		ID:         uuid.New(),
		TransferID: transfer.ID,
		WalletID:   destinationWalletID,
		EntryType:  domain.LedgerEntryTypeCredit,
		Amount:     transfer.Amount,
	}

	if err := s.repo.CreateLedgerEntry(ctx, tx, credit); err != nil {
		return fmt.Errorf("create credit ledger entry: %w", err)
	}

	return nil
}

func (s *TransferService) completeTransfer(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer, record *domain.IdempotencyRecord) (*domain.CreateTransferResult, error) {

	if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.TransferStatusPending, domain.TransferStatusProcessed); err != nil {
		return nil, fmt.Errorf("mark transfer processed: %w", err)
	}

	result := &domain.CreateTransferResult{
		TransferID: transfer.ID,
		Status:     domain.TransferStatusProcessed,
	}

	if err := s.storeIdempotencyResult(ctx, tx, record, result, 201); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *TransferService) storeIdempotencyResult(ctx context.Context, tx *sql.Tx, record *domain.IdempotencyRecord, result *domain.CreateTransferResult, responseCode int) error {

	responseBody, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal transfer response: %w", err)
	}

	record.TransferID = &result.TransferID
	record.ResponseCode = &responseCode
	record.ResponseBody = responseBody

	if err := s.repo.UpdateResult(ctx, tx, record); err != nil {
		return fmt.Errorf("store idempotency response: %w", err)
	}

	return nil
}

func (s *TransferService) handleExistingIdempotencyRecord(ctx context.Context, tx *sql.Tx, key string, requestHash string) (*domain.CreateTransferResult, error) {

	record, err := s.repo.GetIdempotencyRecord(ctx, tx, key)
	if err != nil {
		return nil, err
	}

	if record.RequestHash != requestHash {
		return nil, domain.ErrIdempotencyConflict
	}

	if len(record.ResponseBody) == 0 {
		return nil, fmt.Errorf("idempotency record has no stored response")
	}

	var result domain.CreateTransferResult

	if err := json.Unmarshal(record.ResponseBody, &result); err != nil {
		return nil, fmt.Errorf("decode idempotency response: %w", err)
	}

	result.Replayed = true

	// Replay original business outcome.
	if record.ResponseCode != nil && *record.ResponseCode == 422 {
		return &result, domain.ErrInsufficientFunds
	}

	return &result, nil
}

func (s *TransferService) lockWallets(ctx context.Context, tx *sql.Tx, fromWalletID uuid.UUID, toWalletID uuid.UUID) (*domain.Wallet, *domain.Wallet, error) {

	firstID := fromWalletID
	secondID := toWalletID

	if firstID.String() > secondID.String() {
		firstID, secondID = secondID, firstID
	}

	firstWallet, err := s.repo.GetWalletForUpdate(ctx, tx, firstID)
	if err != nil {
		return nil, nil, err
	}

	secondWallet, err := s.repo.GetWalletForUpdate(ctx, tx, secondID)
	if err != nil {
		return nil, nil, err
	}

	if firstWallet.ID == fromWalletID {
		return firstWallet, secondWallet, nil
	}

	return secondWallet, firstWallet, nil
}

func (s *TransferService) failInsufficientFunds(ctx context.Context, tx *sql.Tx, transfer *domain.Transfer, record *domain.IdempotencyRecord) (*domain.CreateTransferResult, error) {

	if err := s.repo.UpdateTransferStatus(ctx, tx, transfer.ID, domain.TransferStatusPending, domain.TransferStatusFailed); err != nil {
		return nil, err
	}

	result := &domain.CreateTransferResult{
		TransferID: transfer.ID,
		Status:     domain.TransferStatusFailed,
	}

	responseBody, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("marshal failed transfer response: %w", err)
	}

	responseCode := 422

	record.TransferID = &transfer.ID
	record.ResponseCode = &responseCode
	record.ResponseBody = responseBody

	if err := s.repo.UpdateResult(ctx, tx, record); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit failed transfer: %w", err)
	}

	return result, domain.ErrInsufficientFunds
}
