package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
)

type TransferResult struct {
	TransferID   uuid.UUID             `json:"transferId"`
	Status       domain.TransferStatus `json:"status"`
	FromWalletID string                `json:"fromWalletId"`
	ToWalletID   string                `json:"toWalletId"`
	Amount       domain.Amount         `json:"amount"`
	Currency     string                `json:"currency"`
	CreatedAt    time.Time             `json:"createdAt"`
}

type TransferService struct {
	tx          TxManager
	wallets     WalletRepository
	transfers   TransferRepository
	ledger      LedgerRepository
	idempotency IdempotencyRepository
	newID       func() uuid.UUID
}

func NewTransferService(
	tx TxManager,
	wallets WalletRepository,
	transfers TransferRepository,
	ledger LedgerRepository,
	idempotency IdempotencyRepository,
) *TransferService {
	return &TransferService{
		tx:          tx,
		wallets:     wallets,
		transfers:   transfers,
		ledger:      ledger,
		idempotency: idempotency,
		newID:       uuid.New,
	}
}

func (s *TransferService) Transfer(ctx context.Context, req domain.TransferRequest) (TransferResult, error) {
	req = req.Normalize()
	if err := req.Validate(); err != nil {
		return TransferResult{}, err
	}

	var result TransferResult
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.executeTransfer(ctx, req)
		return err
	})
	if err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

func (s *TransferService) executeTransfer(ctx context.Context, req domain.TransferRequest) (TransferResult, error) {
	source, destination, err := s.wallets.LockPair(ctx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return TransferResult{}, err
	}

	if source.Currency != destination.Currency {
		return TransferResult{}, fmt.Errorf("%w: %s cannot be transferred to a %s wallet", domain.ErrCurrencyMismatch, source.Currency, destination.Currency)
	}

	transfer, err := s.recordTransfer(ctx, req, source, destination)
	if err != nil {
		return TransferResult{}, err
	}

	result := resultFrom(transfer)
	if err := s.claimKey(ctx, req, result); err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

func (s *TransferService) recordTransfer(
	ctx context.Context, req domain.TransferRequest, source, destination domain.Wallet,
) (*domain.Transfer, error) {
	sourceBalance, err := source.Debit(req.Amount)
	if err != nil {
		return nil, err
	}
	destinationBalance, err := destination.Credit(req.Amount)
	if err != nil {
		return nil, err
	}

	transfer, err := domain.NewPendingTransfer(s.newID(), req, source.Currency)
	if err != nil {
		return nil, err
	}
	if err := s.transfers.Create(ctx, transfer); err != nil {
		return nil, err
	}
	if err := s.ledger.CreatePair(ctx, domain.LedgerEntriesFor(transfer)); err != nil {
		return nil, err
	}

	if err := s.wallets.UpdateBalance(ctx, source.ID, sourceBalance); err != nil {
		return nil, err
	}
	if err := s.wallets.UpdateBalance(ctx, destination.ID, destinationBalance); err != nil {
		return nil, err
	}

	if err := s.markProcessed(ctx, transfer); err != nil {
		return nil, err
	}
	return transfer, nil
}

func (s *TransferService) markProcessed(ctx context.Context, transfer *domain.Transfer) error {
	previous := transfer.Status
	if err := transfer.MarkProcessed(); err != nil {
		return err
	}
	return s.transfers.UpdateStatus(ctx, transfer.ID, previous, transfer.Status, nil)
}

func (s *TransferService) claimKey(ctx context.Context, req domain.TransferRequest, result TransferResult) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode idempotent response: %w", err)
	}

	return s.idempotency.Create(ctx, domain.IdempotencyRecord{
		Key:          req.IdempotencyKey,
		Fingerprint:  req.Fingerprint(),
		TransferID:   result.TransferID,
		ResponseBody: body,
	})
}

func resultFrom(t *domain.Transfer) TransferResult {
	return TransferResult{
		TransferID:   t.ID,
		Status:       t.Status,
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount,
		Currency:     t.Currency,
		CreatedAt:    t.CreatedAt,
	}
}
