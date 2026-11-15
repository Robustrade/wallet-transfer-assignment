package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

const lockTimeout = 5 * time.Second

type TransferService struct{}

func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (*domain.Transfer, bool, error) {
	transfer, err := domain.NewTransfer(uuid.NewString(), req)
	if err != nil {
		return nil, false, err
	}
	requestHash := hashTransferRequest(req)

	var result *domain.Transfer
	var replayed bool

	err = database.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repository.Repo.SetLockTimeout(tx, lockTimeout); err != nil {
			return err
		}

		claimed, err := repository.Repo.ClaimIdempotencyKey(tx, req.IdempotencyKey, requestHash)
		if err != nil {
			return err
		}

		if !claimed {
			original, err := findOriginalTransfer(tx, req.IdempotencyKey, requestHash)
			if err != nil {
				return err
			}
			result, replayed = original, true
			return nil
		}

		if err := executeTransfer(tx, transfer); err != nil {
			return err
		}
		result = transfer
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return result, replayed, nil
}

func findOriginalTransfer(tx *gorm.DB, key, requestHash string) (*domain.Transfer, error) {
	storedHash, err := repository.Repo.GetIdempotencyRequestHash(tx, key)
	if err != nil {
		return nil, err
	}
	if storedHash != requestHash {
		return nil, domain.ErrIdempotencyKeyReused
	}
	return repository.Repo.GetTransferByIdempotencyKey(tx, key)
}

func executeTransfer(tx *gorm.DB, transfer *domain.Transfer) error {
	wallets, err := repository.Repo.LockWallets(tx, []string{transfer.FromWalletID, transfer.ToWalletID})
	if err != nil {
		return err
	}
	if len(wallets) != 2 {
		return domain.ErrWalletNotFound
	}
	from, to := wallets[0], wallets[1]
	if from.ID != transfer.FromWalletID {
		from, to = to, from
	}

	if err := repository.Repo.CreateTransfer(tx, transfer); err != nil {
		return err
	}

	if err := from.Debit(transfer.Amount); err != nil {
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			return err
		}
		if err := transfer.MarkFailed(domain.FailureInsufficientFunds); err != nil {
			return err
		}
		return repository.Repo.UpdateTransferStatus(tx, transfer)
	}
	if err := to.Credit(transfer.Amount); err != nil {
		return err
	}

	if err := repository.Repo.UpdateWalletBalance(tx, from); err != nil {
		return err
	}
	if err := repository.Repo.UpdateWalletBalance(tx, to); err != nil {
		return err
	}

	if err := transfer.MarkProcessed(); err != nil {
		return err
	}
	entries, err := transfer.LedgerEntries()
	if err != nil {
		return err
	}
	if err := repository.Repo.CreateLedgerEntries(tx, entries); err != nil {
		return err
	}
	return repository.Repo.UpdateTransferStatus(tx, transfer)
}

func hashTransferRequest(req domain.TransferRequest) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q|%q|%d", req.FromWalletID, req.ToWalletID, req.Amount)))
	return hex.EncodeToString(sum[:])
}
