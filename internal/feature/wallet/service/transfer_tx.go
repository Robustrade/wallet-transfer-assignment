package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"

	"wallet-transfer-assignment/internal/feature/wallet/domain"

	"gorm.io/gorm"
)

const failureInsufficientFunds = "INSUFFICIENT_FUNDS"

// executeTransfer is the body of the transfer transaction:
//
//  1. claim the idempotency key (or replay the stored outcome),
//  2. lock both wallets in ascending id order,
//  3. insert the PENDING transfer,
//  4. reject for insufficient funds, or move the money, write the ledger pair
//     and mark the transfer PROCESSED,
//  5. store the response so duplicates can replay it.
//
// Everything commits together or not at all.
func (s *transferService) executeTransfer(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer, requestHash string) (*TransferResult, error) {
	record := &domain.IdempotencyRecord{
		IdempotencyKey: transfer.IdempotencyKey,
		RequestHash:    requestHash,
	}

	replay, claimed, err := s.claimKey(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	if !claimed {
		return replay, nil
	}

	// Lock wallets BEFORE inserting the transfer: the FK on transfers takes
	// FOR KEY SHARE on both wallets, which deadlocks with concurrent FOR UPDATE.
	sender, receiver, err := s.lockWallets(ctx, tx, transfer)
	if err != nil {
		return nil, err
	}

	if err := s.createPendingTransfer(ctx, tx, transfer); err != nil {
		return nil, err
	}

	if err := sender.CanDebit(transfer.Amount); err != nil {
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			return nil, err
		}
		return s.rejectInsufficientFunds(ctx, tx, transfer, record)
	}

	return s.applyTransfer(ctx, tx, transfer, record, sender, receiver)
}

// claimKey inserts the idempotency record. When the key already exists it
// returns the stored result instead (claimed == false).
func (s *transferService) claimKey(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord) (result *TransferResult, claimed bool, err error) {
	created, err := s.idempotencyRepo.Create(ctx, tx, record)
	if err != nil {
		return nil, false, err
	}
	if created {
		return nil, true, nil
	}

	result, err = s.loadSavedResponse(ctx, tx, record.IdempotencyKey, record.RequestHash)
	if err != nil {
		return nil, false, err
	}

	return result, false, nil
}

// loadSavedResponse returns the stored outcome for an existing key. The same
// key with a different payload is a conflict, and a key without a stored
// response is still in progress; neither is ever executed again.
func (s *transferService) loadSavedResponse(ctx context.Context, tx *gorm.DB, key, requestHash string) (*TransferResult, error) {
	existing, err := s.idempotencyRepo.GetByKey(ctx, tx, key)
	if err != nil {
		return nil, err
	}

	if err := existing.MatchesRequest(requestHash); err != nil {
		return nil, err
	}

	if existing.ResponseStatus == nil || existing.ResponseBody == nil {
		return nil, ErrRequestInProgress
	}

	return &TransferResult{
		StatusCode: *existing.ResponseStatus,
		Body:       canonicalJSON(existing.ResponseBody),
	}, nil
}

// lockWallets locks both wallets with SELECT ... FOR UPDATE in ascending id
// order regardless of transfer direction (this avoids A->B / B->A deadlocks)
// and returns them as sender and receiver.
func (s *transferService) lockWallets(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) (sender, receiver *domain.Wallet, err error) {
	firstID, secondID := transfer.FromWalletID, transfer.ToWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	first, err := s.walletRepo.GetByIDForUpdate(ctx, tx, firstID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock first wallet: %w", err)
	}

	second, err := s.walletRepo.GetByIDForUpdate(ctx, tx, secondID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock second wallet: %w", err)
	}

	if transfer.FromWalletID == firstID {
		return first, second, nil
	}

	return second, first, nil
}

func (s *transferService) createPendingTransfer(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error {
	created, err := s.transferRepo.Create(ctx, tx, transfer)
	if err != nil {
		return fmt.Errorf("create transfer: %w", err)
	}
	if !created {
		return errors.New("transfer already exists")
	}

	return nil
}

// rejectInsufficientFunds records a FAILED transfer and its replayable 409
// response. It returns a result (not an error) so the transaction commits.
func (s *transferService) rejectInsufficientFunds(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer, record *domain.IdempotencyRecord) (*TransferResult, error) {
	if err := transfer.ChangeStatus(domain.TransferFailed); err != nil {
		return nil, err
	}

	reason := failureInsufficientFunds
	transfer.FailureReason = &reason

	if err := s.transferRepo.UpdateStatus(ctx, tx, transfer); err != nil {
		return nil, err
	}

	result := &TransferResult{
		StatusCode: http.StatusConflict,
		Body:       []byte(`{"error":"insufficient funds"}`),
	}

	if err := s.saveResponse(ctx, tx, record, transfer.ID, result); err != nil {
		return nil, err
	}

	return result, nil
}

// applyTransfer moves the money, writes the debit/credit pair, marks the
// transfer PROCESSED and stores the 201 response.
func (s *transferService) applyTransfer(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer, record *domain.IdempotencyRecord, sender, receiver *domain.Wallet) (*TransferResult, error) {
	if err := s.moveFunds(ctx, tx, sender, receiver, transfer.Amount); err != nil {
		return nil, err
	}

	entries, err := domain.NewLedgerEntries(transfer.ID, sender.ID, receiver.ID, transfer.Amount)
	if err != nil {
		return nil, fmt.Errorf("create ledger entries: %w", err)
	}
	if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
		return nil, fmt.Errorf("save ledger entries: %w", err)
	}

	if err := transfer.ChangeStatus(domain.TransferProcessed); err != nil {
		return nil, fmt.Errorf("change transfer status: %w", err)
	}
	if err := s.transferRepo.UpdateStatus(ctx, tx, transfer); err != nil {
		return nil, fmt.Errorf("update transfer status: %w", err)
	}

	result, err := newProcessedResult(transfer)
	if err != nil {
		return nil, err
	}
	if err := s.saveResponse(ctx, tx, record, transfer.ID, result); err != nil {
		return nil, err
	}

	return result, nil
}

// moveFunds debits the sender and credits the receiver, refusing to overflow
// the receiver's balance.
func (s *transferService) moveFunds(ctx context.Context, tx *gorm.DB, sender, receiver *domain.Wallet, amount int64) error {
	if receiver.Balance > math.MaxInt64-amount {
		return errors.New("receiver balance overflow")
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, sender.ID, -amount); err != nil {
		return fmt.Errorf("debit sender: %w", err)
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, receiver.ID, amount); err != nil {
		return fmt.Errorf("credit receiver: %w", err)
	}

	return nil
}

// saveResponse stores the outcome on the idempotency record for later replay.
func (s *transferService) saveResponse(ctx context.Context, tx *gorm.DB, record *domain.IdempotencyRecord, transferID int64, result *TransferResult) error {
	status := result.StatusCode

	record.TransferID = &transferID
	record.ResponseStatus = &status
	record.ResponseBody = result.Body

	if err := s.idempotencyRepo.SaveResponse(ctx, tx, record); err != nil {
		return fmt.Errorf("save idempotency response: %w", err)
	}

	return nil
}
