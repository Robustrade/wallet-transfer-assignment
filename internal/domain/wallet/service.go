package wallet

import (
	"context"
	"errors"
	"fmt"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/timeutil"
)

// ErrNotFound is returned when a wallet lookup yields no result.
var ErrNotFound = errors.New("wallet: not found")

// ErrInactive is returned when an operation targets a non-active wallet.
var ErrInactive = errors.New("wallet: wallet is inactive")

// Service contains the business logic for the wallet domain.
type Service struct {
	repo Repository
}

// NewService constructs a WalletService with the given repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// GetByID fetches a single wallet by ID.
func (s *Service) GetByID(ctx context.Context, id string) (*Wallet, error) {
	w, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("wallet.Service.GetByID: %w", err)
	}
	return w, nil
}

// Create constructs, validates, and persists a new wallet.
// The ID is domain-generated (prefixed UUID v7) before any DB interaction.
func (s *Service) Create(ctx context.Context, ownerID, currency string) (*Wallet, error) {
	now := timeutil.Now()
	w := &Wallet{
		ID:        NewID(), // "wallet_<uuidv7>" — domain-owned, time-ordered
		OwnerID:   ownerID,
		Currency:  currency,
		Status:    StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := w.ValidateForCreate(); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, w); err != nil {
		return nil, fmt.Errorf("wallet.Service.Create: %w", err)
	}
	return w, nil
}
