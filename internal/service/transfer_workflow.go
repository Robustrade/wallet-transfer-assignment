package service

import (
	"context"
	"fmt"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
)

func newTransferID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate transfer id: %w", err)
	}
	return id.String(), nil
}

// executeTransfer runs the transfer workflow inside an open transaction.
func (s *TransferServiceImpl) executeTransfer(
	ctx context.Context,
	input CreateTransferInput,
	transferID string,
) (*CreateTransferResult, error) {
	source, target, err := s.lockWalletsForTransfer(ctx, input.FromWalletID, input.ToWalletID)
	if err != nil {
		return nil, err
	}

	if err := validateWalletsForTransfer(source, target, input.Amount); err != nil {
		return nil, err
	}

	if err := s.createPendingTransfer(ctx, transferID, input, source.Currency); err != nil {
		return nil, err
	}

	if err := s.recordLedgerEntries(ctx, transferID, input); err != nil {
		return nil, err
	}

	if err := s.applyBalanceChanges(ctx, source, target, input.Amount); err != nil {
		return nil, err
	}

	if err := s.markTransferProcessed(ctx, transferID); err != nil {
		return nil, err
	}

	result := buildTransferResult(transferID, input.Amount, source.Currency)

	if err := s.saveIdempotencyRecord(ctx, input, result); err != nil {
		return nil, err
	}

	return result, nil
}

// lockWalletsForTransfer locks source and target rows in a stable order to avoid deadlocks.
func (s *TransferServiceImpl) lockWalletsForTransfer(
	ctx context.Context,
	fromWalletID, toWalletID string,
) (*domain.Wallet, *domain.Wallet, error) {
	firstID, secondID := fromWalletID, toWalletID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	first, err := s.store.Wallets().LockByID(ctx, firstID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock wallet %s: %w", firstID, err)
	}

	second, err := s.store.Wallets().LockByID(ctx, secondID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock wallet %s: %w", secondID, err)
	}

	if fromWalletID == firstID {
		return first, second, nil
	}
	return second, first, nil
}

func validateWalletsForTransfer(source, target *domain.Wallet, amount int64) error {
	if source.Currency != target.Currency {
		return domain.ErrInvalidTransfer
	}
	if source.Balance < amount {
		return domain.ErrInsufficientFunds
	}
	return nil
}

func (s *TransferServiceImpl) createPendingTransfer(
	ctx context.Context,
	transferID string,
	input CreateTransferInput,
	currency string,
) error {
	return s.store.Transfers().Create(ctx, &domain.Transfer{
		ID:             transferID,
		SourceWalletID: input.FromWalletID,
		TargetWalletID: input.ToWalletID,
		Amount:         input.Amount,
		Currency:       currency,
		State:          domain.TransferStatePending,
	})
}

func (s *TransferServiceImpl) recordLedgerEntries(
	ctx context.Context,
	transferID string,
	input CreateTransferInput,
) error {
	if err := s.store.Ledger().Create(ctx, &domain.LedgerEntry{
		WalletID:   input.FromWalletID,
		TransferID: transferID,
		Type:       domain.LedgerEntryTypeDebit,
		Amount:     input.Amount,
	}); err != nil {
		return fmt.Errorf("record debit: %w", err)
	}

	if err := s.store.Ledger().Create(ctx, &domain.LedgerEntry{
		WalletID:   input.ToWalletID,
		TransferID: transferID,
		Type:       domain.LedgerEntryTypeCredit,
		Amount:     input.Amount,
	}); err != nil {
		return fmt.Errorf("record credit: %w", err)
	}
	return nil
}

func (s *TransferServiceImpl) applyBalanceChanges(
	ctx context.Context,
	source, target *domain.Wallet,
	amount int64,
) error {
	source.Balance -= amount
	target.Balance += amount

	if err := s.store.Wallets().UpdateBalance(ctx, source); err != nil {
		return fmt.Errorf("update source balance: %w", err)
	}
	if err := s.store.Wallets().UpdateBalance(ctx, target); err != nil {
		return fmt.Errorf("update target balance: %w", err)
	}
	return nil
}

func (s *TransferServiceImpl) markTransferProcessed(ctx context.Context, transferID string) error {
	return s.store.Transfers().UpdateState(ctx, transferID, domain.TransferStateProcessed)
}

func buildTransferResult(transferID string, amount int64, currency string) *CreateTransferResult {
	return &CreateTransferResult{
		TransferID: transferID,
		State:      domain.TransferStateProcessed,
		Amount:     amount,
		Currency:   currency,
	}
}
