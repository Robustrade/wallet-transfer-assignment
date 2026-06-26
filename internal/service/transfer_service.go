package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
)

// Service is the concrete orchestration of the TransferService contract. It
// coordinates the repositories under a single transaction so that wallet
// balances, the double-entry ledger, the transfer record and the idempotency
// token all move together (or not at all).
type Service struct {
	wallets     WalletRepository
	transfers   TransferRepository
	ledger      LedgerRepository
	idempotency IdempotencyRepository
	txManager   TxManager

	// newID produces identifiers for transfers and ledger entries. It is a field
	// so tests can inject deterministic values via WithIDGenerator.
	newID func() string
}

var _ TransferService = (*Service)(nil)

// Option customises a Service at construction time.
type Option func(*Service)

// WithIDGenerator overrides the identifier generator. The default is
// uuid.NewString; tests use this to make ids deterministic.
func WithIDGenerator(gen func() string) Option {
	return func(s *Service) {
		if gen != nil {
			s.newID = gen
		}
	}
}

// New builds a Service from the bundled repositories and optional overrides.
func New(r Repositories, opts ...Option) *Service {
	s := &Service{
		wallets:     r.Wallets,
		transfers:   r.Transfers,
		ledger:      r.Ledger,
		idempotency: r.Idempotency,
		txManager:   r.TxManager,
		newID:       uuid.NewString,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// transferSnapshot is the stable, auditable projection of a transfer that is
// stored in the idempotency response body for replay.
type transferSnapshot struct {
	ID            string                `json:"id"`
	Status        domain.TransferStatus `json:"status"`
	FromWalletID  string                `json:"from_wallet_id"`
	ToWalletID    string                `json:"to_wallet_id"`
	Amount        domain.Money          `json:"amount"`
	FailureReason string                `json:"failure_reason,omitempty"`
}

func snapshotJSON(t *domain.Transfer) ([]byte, error) {
	return json.Marshal(transferSnapshot{
		ID:            t.ID,
		Status:        t.Status,
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		FailureReason: t.FailureReason,
	})
}

// requestHash is the hex sha256 of the semantically significant request fields.
func requestHash(from, to string, amount domain.Money) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", from, to, amount)))
	return hex.EncodeToString(sum[:])
}

// Transfer executes (or replays) a wallet-to-wallet transfer with exactly-once
// semantics keyed on in.IdempotencyKey.
func (s *Service) Transfer(ctx context.Context, in TransferInput) (*TransferResult, error) {
	// 1. Validate up front, no side effects.
	if in.IdempotencyKey == "" {
		return nil, domain.ErrMissingIdempotencyKey
	}
	if in.FromWalletID == "" || in.ToWalletID == "" {
		return nil, domain.ErrMissingWalletID
	}
	if in.FromWalletID == in.ToWalletID {
		return nil, domain.ErrSameWallet
	}
	if err := domain.ValidateAmount(in.Amount); err != nil {
		return nil, err
	}

	// 2. Canonical request hash for duplicate-payload detection.
	hash := requestHash(in.FromWalletID, in.ToWalletID, in.Amount)

	var result *TransferResult

	// 3. Everything durable happens inside one transaction.
	err := s.txManager.WithinTx(ctx, func(ctx context.Context) error {
		// a. Claim the idempotency key.
		rec := &domain.IdempotencyRecord{
			Key:         in.IdempotencyKey,
			RequestHash: hash,
			Status:      domain.IdempotencyPending,
		}
		insertErr := s.idempotency.Insert(ctx, rec)

		// b. Duplicate key: replay the original outcome.
		if errors.Is(insertErr, domain.ErrDuplicateIdempotencyKey) {
			existing, err := s.idempotency.Get(ctx, in.IdempotencyKey)
			if err != nil {
				return err
			}
			if existing.RequestHash != hash {
				return domain.ErrIdempotencyKeyReuse
			}
			transfer, err := s.transfers.Get(ctx, existing.TransferID)
			if err != nil {
				return err
			}
			result = &TransferResult{Transfer: transfer, Replayed: true}
			return nil
		}

		// c. Any other insert error aborts.
		if insertErr != nil {
			return insertErr
		}

		// d. We own the key: create the PENDING transfer.
		transferID := s.newID()
		transfer, err := domain.NewTransfer(transferID, in.IdempotencyKey, in.FromWalletID, in.ToWalletID, in.Amount)
		if err != nil {
			return err
		}
		if err := s.transfers.Create(ctx, transfer); err != nil {
			return err
		}

		// e. Lock both wallets in a deterministic (sorted) order to avoid
		// deadlocks between opposing transfers.
		source, dest, err := s.lockWallets(ctx, in.FromWalletID, in.ToWalletID)
		if err != nil {
			return err
		}

		// f. Debit the source. Insufficient funds is a durable, idempotent
		// business failure: record it and COMMIT, do not return the error.
		if debitErr := source.Debit(in.Amount); debitErr != nil {
			if errors.Is(debitErr, domain.ErrInsufficientFunds) {
				if err := transfer.MarkFailed("insufficient funds"); err != nil {
					return err
				}
				if err := s.transfers.Update(ctx, transfer); err != nil {
					return err
				}
				if err := s.complete(ctx, in.IdempotencyKey, transfer); err != nil {
					return err
				}
				result = &TransferResult{Transfer: transfer, Replayed: false}
				return nil
			}
			return debitErr
		}

		// g. Successful debit: credit dest, persist balances, write the ledger
		// pair, mark processed, finalise idempotency.
		if err := dest.Credit(in.Amount); err != nil {
			return err
		}
		if err := s.wallets.UpdateBalance(ctx, source.ID, source.Balance); err != nil {
			return err
		}
		if err := s.wallets.UpdateBalance(ctx, dest.ID, dest.Balance); err != nil {
			return err
		}
		entries := domain.NewDoubleEntry(s.newID(), s.newID(), transfer.ID, in.FromWalletID, in.ToWalletID, in.Amount)
		if err := s.ledger.CreateEntries(ctx, entries); err != nil {
			return err
		}
		if err := transfer.MarkProcessed(); err != nil {
			return err
		}
		if err := s.transfers.Update(ctx, transfer); err != nil {
			return err
		}
		if err := s.complete(ctx, in.IdempotencyKey, transfer); err != nil {
			return err
		}
		result = &TransferResult{Transfer: transfer, Replayed: false}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 4. Surface a failed outcome as an error while still returning the durable
	// FAILED transfer (applies to both fresh failures and replays of one).
	if result.Transfer.Status == domain.StatusFailed {
		return result, domain.ErrInsufficientFunds
	}
	return result, nil
}

// lockWallets acquires the row locks for both wallets in a deterministic order
// (smaller id first) and maps them back to source/destination.
func (s *Service) lockWallets(ctx context.Context, from, to string) (source, dest *domain.Wallet, err error) {
	ids := []string{from, to}
	sort.Strings(ids)

	first, err := s.wallets.GetForUpdate(ctx, ids[0])
	if err != nil {
		return nil, nil, err
	}
	second, err := s.wallets.GetForUpdate(ctx, ids[1])
	if err != nil {
		return nil, nil, err
	}

	byID := map[string]*domain.Wallet{first.ID: first, second.ID: second}
	source = byID[from]
	dest = byID[to]
	if source == nil || dest == nil {
		return nil, nil, domain.ErrWalletNotFound
	}
	return source, dest, nil
}

// complete writes the auditable snapshot and finalises the idempotency record.
func (s *Service) complete(ctx context.Context, key string, transfer *domain.Transfer) error {
	response, err := snapshotJSON(transfer)
	if err != nil {
		return err
	}
	return s.idempotency.Complete(ctx, key, transfer.ID, response)
}

// GetTransfer returns a transfer by id.
func (s *Service) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.transfers.Get(ctx, id)
}

// GetWalletBalance returns the current balance of a wallet.
func (s *Service) GetWalletBalance(ctx context.Context, walletID string) (*WalletBalance, error) {
	wallet, err := s.wallets.Get(ctx, walletID)
	if err != nil {
		return nil, err
	}
	return &WalletBalance{WalletID: wallet.ID, Balance: wallet.Balance}, nil
}
