package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/ports"
)

type TransferService struct {
	store ports.TransactionalStore
	clock func() time.Time
	newID func(string) (string, error)
}

func NewTransferService(store ports.TransactionalStore) *TransferService {
	return &TransferService{
		store: store,
		clock: time.Now,
		newID: randomID,
	}
}

func (s *TransferService) CreateWallet(ctx context.Context, input domain.CreateWalletInput) (domain.Wallet, error) {
	input = domain.NormalizeCreateWalletInput(input)
	if err := domain.ValidateCreateWalletInput(input); err != nil {
		return domain.Wallet{}, err
	}

	now := s.clock().UTC()
	wallet := domain.Wallet{
		ID:        input.ID,
		Balance:   input.InitialBalance,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := s.store.WithTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		return repo.CreateWallet(ctx, wallet)
	})
	if err != nil {
		return domain.Wallet{}, err
	}

	return wallet, nil
}

func (s *TransferService) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	var wallet domain.Wallet

	err := s.store.WithReadTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		foundWallet, found, err := repo.GetWallet(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return domain.NewAppError(domain.ErrorWalletNotFound, "wallet not found")
		}
		wallet = foundWallet
		return nil
	})
	if err != nil {
		return domain.Wallet{}, err
	}

	return wallet, nil
}

func (s *TransferService) GetTransfer(ctx context.Context, id string) (domain.TransferResult, error) {
	var result domain.TransferResult

	err := s.store.WithReadTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		transfer, found, err := repo.FindTransferByID(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			return domain.NewAppError(domain.ErrorTransferNotFound, "transfer not found")
		}

		entries, err := repo.ListLedgerEntriesByTransfer(ctx, transfer.ID)
		if err != nil {
			return err
		}

		result = domain.TransferResult{
			Transfer:      transfer,
			LedgerEntries: entries,
		}
		return nil
	})
	if err != nil {
		return domain.TransferResult{}, err
	}

	return result, nil
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	input domain.CreateTransferInput,
) (domain.TransferResult, error) {
	input = domain.NormalizeCreateTransferInput(input)
	if err := domain.ValidateCreateTransferInput(input); err != nil {
		return domain.TransferResult{}, err
	}

	requestHash := transferRequestHash(input)
	var result domain.TransferResult

	err := s.store.WithTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		existing, found, err := repo.FindTransferByIdempotencyKey(ctx, input.IdempotencyKey)
		if err != nil {
			return err
		}
		if found {
			if existing.RequestHash != requestHash {
				return domain.NewAppError(
					domain.ErrorIdempotencyConflict,
					"idempotencyKey was already used with different transfer parameters",
				)
			}

			entries, err := repo.ListLedgerEntriesByTransfer(ctx, existing.ID)
			if err != nil {
				return err
			}
			result = domain.TransferResult{
				Transfer:         existing,
				LedgerEntries:    entries,
				IdempotentReplay: true,
			}
			return nil
		}

		transferID, err := s.newID("tr")
		if err != nil {
			return fmt.Errorf("generate transfer id: %w", err)
		}

		now := s.clock().UTC()
		transfer := domain.Transfer{
			ID:             transferID,
			IdempotencyKey: input.IdempotencyKey,
			RequestHash:    requestHash,
			FromWalletID:   input.FromWalletID,
			ToWalletID:     input.ToWalletID,
			Amount:         input.Amount,
			State:          domain.TransferPending,
			CreatedAt:      now,
			UpdatedAt:      now,
		}

		if err = repo.InsertTransfer(ctx, transfer); err != nil {
			return err
		}

		operationTime := s.clock().UTC()

		_, sourceFound, err := repo.GetWallet(ctx, input.FromWalletID)
		if err != nil {
			return err
		}
		if !sourceFound {
			return s.failTransfer(ctx, repo, transfer.ID, domain.ErrorSourceWalletNotFound, operationTime, &result)
		}

		_, destinationFound, err := repo.GetWallet(ctx, input.ToWalletID)
		if err != nil {
			return err
		}
		if !destinationFound {
			return s.failTransfer(ctx, repo, transfer.ID, domain.ErrorDestinationWalletNotFound, operationTime, &result)
		}

		debited, err := repo.DebitWallet(ctx, input.FromWalletID, input.Amount, operationTime)
		if err != nil {
			return err
		}
		if !debited {
			return s.failTransfer(ctx, repo, transfer.ID, domain.ErrorInsufficientFunds, operationTime, &result)
		}

		if err = repo.CreditWallet(ctx, input.ToWalletID, input.Amount, operationTime); err != nil {
			return err
		}

		entries := []domain.LedgerEntry{
			{
				WalletID:   input.FromWalletID,
				TransferID: transfer.ID,
				Type:       domain.LedgerDebit,
				Amount:     input.Amount,
				CreatedAt:  operationTime,
			},
			{
				WalletID:   input.ToWalletID,
				TransferID: transfer.ID,
				Type:       domain.LedgerCredit,
				Amount:     input.Amount,
				CreatedAt:  operationTime,
			},
		}
		if err = repo.InsertLedgerEntries(ctx, entries); err != nil {
			return err
		}

		processedTransfer, err := repo.UpdateTransferState(
			ctx,
			transfer.ID,
			domain.TransferProcessed,
			"",
			operationTime,
		)
		if err != nil {
			return err
		}

		persistedEntries, err := repo.ListLedgerEntriesByTransfer(ctx, transfer.ID)
		if err != nil {
			return err
		}

		result = domain.TransferResult{
			Transfer:      processedTransfer,
			LedgerEntries: persistedEntries,
		}
		return nil
	})
	if err != nil {
		return domain.TransferResult{}, err
	}

	return result, nil
}

func (s *TransferService) failTransfer(
	ctx context.Context,
	repo ports.TransferRepository,
	transferID string,
	errorCode domain.ErrorCode,
	updatedAt time.Time,
	result *domain.TransferResult,
) error {
	failedTransfer, err := repo.UpdateTransferState(
		ctx,
		transferID,
		domain.TransferFailed,
		errorCode,
		updatedAt,
	)
	if err != nil {
		return err
	}

	entries, err := repo.ListLedgerEntriesByTransfer(ctx, transferID)
	if err != nil {
		return err
	}

	*result = domain.TransferResult{
		Transfer:      failedTransfer,
		LedgerEntries: entries,
	}
	return nil
}

func transferRequestHash(input domain.CreateTransferInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s\x00%s\x00%d",
		input.FromWalletID,
		input.ToWalletID,
		input.Amount,
	)))

	return hex.EncodeToString(sum[:])
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(bytes)), nil
}
