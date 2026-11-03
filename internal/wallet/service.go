package wallet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

type Service struct {
	repository *Repository
	now        func() time.Time
	newID      func() (string, error)
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
		now:        time.Now,
		newID:      newUUID,
	}
}

type CreateWalletRequest struct {
	WalletID       *string         `json:"walletId"`
	InitialBalance json.RawMessage `json:"initialBalance"`
}

func (service *Service) CreateWallet(
	ctx context.Context,
	request CreateWalletRequest,
) (Wallet, error) {
	var walletID string
	if request.WalletID == nil {
		id, err := service.newID()
		if err != nil {
			return Wallet{}, fmt.Errorf("generate wallet identifier: %w", err)
		}
		walletID = "wallet_" + strings.ReplaceAll(id, "-", "")
	} else {
		normalized, err := normalizeText(*request.WalletID, "walletId")
		if err != nil {
			return Wallet{}, err
		}
		walletID = normalized
	}

	var balance Money
	if len(request.InitialBalance) > 0 {
		parsed, err := ParseMoney(request.InitialBalance, true)
		if err != nil {
			return Wallet{}, fmt.Errorf("%w: initialBalance is invalid", ErrInvalidRequest)
		}
		balance = parsed
	}
	createdAt := service.now().UTC().Format(time.RFC3339Nano)
	wallet := Wallet{
		WalletID:  walletID,
		Balance:   balance,
		CreatedAt: createdAt,
	}
	err := service.repository.withImmediateTransaction(ctx, func(connection *sql.Conn) error {
		return service.repository.createWallet(ctx, connection, wallet)
	})
	if err != nil {
		return Wallet{}, err
	}
	return wallet, nil
}

func (service *Service) GetWallet(ctx context.Context, walletID string) (Wallet, error) {
	normalized, err := normalizeText(walletID, "walletId")
	if err != nil {
		return Wallet{}, err
	}
	return service.repository.getWallet(ctx, service.repository.db, normalized)
}

func (service *Service) CreateTransfer(
	ctx context.Context,
	request TransferRequest,
) (TransferResult, error) {
	idempotencyKey, err := normalizeText(request.IdempotencyKey, "idempotencyKey")
	if err != nil {
		return TransferResult{}, err
	}
	fromWalletID, err := normalizeText(request.FromWalletID, "fromWalletId")
	if err != nil {
		return TransferResult{}, err
	}
	toWalletID, err := normalizeText(request.ToWalletID, "toWalletId")
	if err != nil {
		return TransferResult{}, err
	}
	if fromWalletID == toWalletID {
		return TransferResult{}, fmt.Errorf("%w: fromWalletId and toWalletId must be different", ErrInvalidRequest)
	}
	if request.Amount <= 0 {
		return TransferResult{}, invalidAmount()
	}

	requestBytes, err := json.Marshal([]any{fromWalletID, toWalletID, int64(request.Amount)})
	if err != nil {
		return TransferResult{}, fmt.Errorf("canonicalize transfer request: %w", err)
	}
	hash := sha256.Sum256(requestBytes)
	requestHash := hex.EncodeToString(hash[:])
	transferID, err := service.newID()
	if err != nil {
		return TransferResult{}, fmt.Errorf("generate transfer identifier: %w", err)
	}
	createdAt := service.now().UTC().Format(time.RFC3339Nano)
	result := TransferResult{
		TransferID:     transferID,
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         request.Amount,
		CreatedAt:      createdAt,
		requestHash:    requestHash,
	}

	err = service.repository.withImmediateTransaction(ctx, func(connection *sql.Conn) error {
		existing, findErr := service.repository.getTransferByKey(ctx, connection, idempotencyKey)
		if findErr == nil {
			if existing.requestHash != requestHash {
				return fmt.Errorf("%w: idempotencyKey has already been used with a different request", ErrIdempotencyReuse)
			}
			result = existing
			return nil
		}
		if !errors.Is(findErr, sql.ErrNoRows) {
			return fmt.Errorf("check idempotency key: %w", findErr)
		}

		source, findErr := service.repository.getWallet(ctx, connection, fromWalletID)
		if findErr != nil {
			return findErr
		}
		destination, findErr := service.repository.getWallet(ctx, connection, toWalletID)
		if findErr != nil {
			return findErr
		}
		if err := service.repository.insertTransfer(ctx, connection, result); err != nil {
			return err
		}

		if source.Balance < request.Amount {
			failureCode := "INSUFFICIENT_FUNDS"
			result.Status = "FAILED"
			result.FailureCode = &failureCode
			if err := service.repository.finishTransfer(ctx, connection, transferID, "FAILED", &failureCode); err != nil {
				return err
			}
		} else if int64(destination.Balance) > math.MaxInt64-int64(request.Amount) {
			failureCode := "BALANCE_LIMIT_EXCEEDED"
			result.Status = "FAILED"
			result.FailureCode = &failureCode
			if err := service.repository.finishTransfer(ctx, connection, transferID, "FAILED", &failureCode); err != nil {
				return err
			}
		} else {
			if err := service.repository.updateBalance(ctx, connection, fromWalletID, source.Balance-request.Amount); err != nil {
				return err
			}
			if err := service.repository.updateBalance(ctx, connection, toWalletID, destination.Balance+request.Amount); err != nil {
				return err
			}
			if err := service.repository.insertLedgerEntry(ctx, connection, fromWalletID, transferID, "DEBIT", request.Amount, createdAt); err != nil {
				return err
			}
			if err := service.repository.insertLedgerEntry(ctx, connection, toWalletID, transferID, "CREDIT", request.Amount, createdAt); err != nil {
				return err
			}
			result.Status = "PROCESSED"
			if err := service.repository.finishTransfer(ctx, connection, transferID, "PROCESSED", nil); err != nil {
				return err
			}
		}
		stored, findErr := service.repository.getTransfer(ctx, connection, transferID)
		if findErr != nil {
			return fmt.Errorf("read completed transfer: %w", findErr)
		}
		result = stored
		return nil
	})
	if err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
