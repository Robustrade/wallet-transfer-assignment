package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/domain"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/repository"
)

type TransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResult struct {
	Transfer *domain.Transfer
	Replayed bool
}

type IDGenerator func() (string, error)
type Clock func() time.Time

type TransferService struct {
	unitOfWork repository.UnitOfWork
	newID      IDGenerator
	now        Clock
}

func NewTransferService(unitOfWork repository.UnitOfWork) *TransferService {
	return NewTransferServiceWithDependencies(unitOfWork, newUUID, time.Now)
}

func NewTransferServiceWithDependencies(
	unitOfWork repository.UnitOfWork,
	newID IDGenerator,
	now Clock,
) *TransferService {
	return &TransferService{unitOfWork: unitOfWork, newID: newID, now: now}
}

func (s *TransferService) CreateTransfer(ctx context.Context, command TransferCommand) (*TransferResult, error) {
	if err := validateCommand(command); err != nil {
		return nil, err
	}

	requestHash := hashCommand(command)
	var result *TransferResult

	err := s.unitOfWork.WithinTransaction(ctx, func(tx repository.Transaction) error {
		claimed, err := tx.TryCreateIdempotency(ctx, command.IdempotencyKey, requestHash)
		if err != nil {
			return fmt.Errorf("claim idempotency key: %w", err)
		}
		if !claimed {
			record, transfer, err := tx.FindTransferByIdempotencyKey(ctx, command.IdempotencyKey)
			if err != nil {
				return fmt.Errorf("load idempotent result: %w", err)
			}
			if record.RequestHash != requestHash {
				return domain.ErrIdempotencyConflict
			}
			result = &TransferResult{Transfer: transfer, Replayed: true}
			return nil
		}

		transferID, err := s.newID()
		if err != nil {
			return fmt.Errorf("generate transfer ID: %w", err)
		}
		now := s.timestamp()
		transfer := domain.NewPendingTransfer(
			transferID,
			command.IdempotencyKey,
			command.FromWalletID,
			command.ToWalletID,
			command.Amount,
			now,
		)
		if err := tx.CreateTransfer(ctx, transfer); err != nil {
			return fmt.Errorf("create pending transfer: %w", err)
		}
		if err := tx.LinkIdempotencyToTransfer(ctx, command.IdempotencyKey, transfer.ID); err != nil {
			return fmt.Errorf("link idempotency record: %w", err)
		}

		wallets, err := tx.LockWallets(ctx, []string{command.FromWalletID, command.ToWalletID})
		if err != nil {
			return fmt.Errorf("lock wallets: %w", err)
		}
		if len(wallets) != 2 {
			if err := transfer.MarkFailed(domain.FailureCodeWalletNotFound, s.timestamp()); err != nil {
				return err
			}
			if err := tx.UpdateTransfer(ctx, transfer); err != nil {
				return fmt.Errorf("record missing wallet failure: %w", err)
			}
			if err := tx.CompleteIdempotency(ctx, command.IdempotencyKey); err != nil {
				return fmt.Errorf("complete idempotency record: %w", err)
			}
			result = &TransferResult{Transfer: transfer}
			return nil
		}

		walletByID := make(map[string]domain.Wallet, len(wallets))
		for _, wallet := range wallets {
			walletByID[wallet.ID] = wallet
		}
		source := walletByID[command.FromWalletID]
		destination := walletByID[command.ToWalletID]

		if source.Balance < command.Amount {
			if err := transfer.MarkFailed(domain.FailureCodeInsufficientBalance, s.timestamp()); err != nil {
				return err
			}
			if err := tx.UpdateTransfer(ctx, transfer); err != nil {
				return fmt.Errorf("record insufficient balance failure: %w", err)
			}
			if err := tx.CompleteIdempotency(ctx, command.IdempotencyKey); err != nil {
				return fmt.Errorf("complete idempotency record: %w", err)
			}
			result = &TransferResult{Transfer: transfer}
			return nil
		}

		if err := tx.UpdateWalletBalance(ctx, source.ID, source.Balance-command.Amount); err != nil {
			return fmt.Errorf("debit source wallet: %w", err)
		}
		if err := tx.UpdateWalletBalance(ctx, destination.ID, destination.Balance+command.Amount); err != nil {
			return fmt.Errorf("credit destination wallet: %w", err)
		}

		debitID, err := s.newID()
		if err != nil {
			return fmt.Errorf("generate debit ledger ID: %w", err)
		}
		creditID, err := s.newID()
		if err != nil {
			return fmt.Errorf("generate credit ledger ID: %w", err)
		}
		entries := []domain.LedgerEntry{
			{
				ID:         debitID,
				WalletID:   source.ID,
				TransferID: transfer.ID,
				Type:       domain.LedgerEntryDebit,
				Amount:     command.Amount,
			},
			{
				ID:         creditID,
				WalletID:   destination.ID,
				TransferID: transfer.ID,
				Type:       domain.LedgerEntryCredit,
				Amount:     command.Amount,
			},
		}
		if err := tx.CreateLedgerEntries(ctx, entries); err != nil {
			return fmt.Errorf("create ledger entries: %w", err)
		}
		if err := transfer.MarkProcessed(s.timestamp()); err != nil {
			return err
		}
		if err := tx.UpdateTransfer(ctx, transfer); err != nil {
			return fmt.Errorf("mark transfer processed: %w", err)
		}
		if err := tx.CompleteIdempotency(ctx, command.IdempotencyKey); err != nil {
			return fmt.Errorf("complete idempotency record: %w", err)
		}
		result = &TransferResult{Transfer: transfer}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *TransferService) timestamp() time.Time {
	// PostgreSQL stores timestamps at microsecond precision. Normalizing before
	// the first response keeps replayed timestamps byte-for-byte stable.
	return s.now().UTC().Truncate(time.Microsecond)
}

func validateCommand(command TransferCommand) error {
	if strings.TrimSpace(command.IdempotencyKey) == "" || len(command.IdempotencyKey) > 255 {
		return domain.ErrInvalidIdempotencyKey
	}
	if strings.TrimSpace(command.FromWalletID) == "" ||
		strings.TrimSpace(command.ToWalletID) == "" ||
		len(command.FromWalletID) > 255 ||
		len(command.ToWalletID) > 255 {
		return domain.ErrInvalidWalletID
	}
	if command.FromWalletID == command.ToWalletID {
		return domain.ErrSameWallet
	}
	if command.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	return nil
}

func hashCommand(command TransferCommand) string {
	payload := fmt.Sprintf("%d:%s|%d:%s|%d",
		len(command.FromWalletID),
		command.FromWalletID,
		len(command.ToWalletID),
		command.ToWalletID,
		command.Amount,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}
