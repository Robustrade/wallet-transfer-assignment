// Package service implements the wallet transfer business logic: validation,
// request identity, the transfer state machine, and the transaction that ties
// a request's outcome to its idempotency key.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"wallet/internal/constants"
	"wallet/internal/domain"
	"wallet/internal/dto"
)

// Result is a completed outcome. For recorded outcomes (201, 422, 404) the
// status and body have already been persisted in idempotency_records, so any
// duplicate of the request replays exactly these bytes.
type Result struct {
	Status int
	Body   []byte
}

// Service is the wallet transfer service.
type Service struct {
	store  Store
	logger *slog.Logger
}

// New builds a Service on top of the given store.
func New(store Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{store: store, logger: logger}
}

// GetWallet returns a wallet's current balance.
func (s *Service) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	if err := validateWalletID("walletId", id); err != nil {
		return domain.Wallet{}, err
	}
	return s.store.GetWallet(ctx, id)
}

// GetTransfer returns a single transfer by id.
func (s *Service) GetTransfer(ctx context.Context, id string) (domain.Transfer, error) {
	if !uuidRe.MatchString(id) {
		return domain.Transfer{}, domain.NewValidationError("transferId must be a UUID")
	}
	return s.store.GetTransfer(ctx, id)
}

// ListLedger returns the most recent ledger entries of a wallet, newest
// first. A limit <= 0 selects the default page size (the HTTP layer rejects
// explicit non-positive limits before reaching the service); larger values
// are clamped to constants.MaxLedgerLimit.
func (s *Service) ListLedger(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error) {
	if err := validateWalletID("walletId", walletID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > constants.MaxLedgerLimit {
		limit = constants.MaxLedgerLimit
	}
	// A ledger query alone cannot distinguish an existing wallet with no
	// history from a nonexistent wallet. Preserve the documented 404 contract.
	if _, err := s.store.GetWallet(ctx, walletID); err != nil {
		return nil, err
	}
	return s.store.ListWalletLedger(ctx, walletID, limit)
}

// uuidRe matches the canonical textual UUID form (any version, any case).
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validate checks every value-level rule of a transfer request. Shape-level
// problems (missing fields, wrong JSON types) are reported by the HTTP layer
// while decoding; both halves return *domain.ValidationError, so the client
// sees one error format.
func validate(req dto.TransferRequest) error {
	if err := validateWalletID("fromWalletId", req.FromWalletID); err != nil {
		return err
	}
	if err := validateWalletID("toWalletId", req.ToWalletID); err != nil {
		return err
	}
	if req.FromWalletID == req.ToWalletID {
		return domain.NewValidationError("fromWalletId and toWalletId must be different wallets")
	}

	if err := validateKey(req.IdempotencyKey); err != nil {
		return err
	}

	if req.Amount <= 0 {
		return domain.NewValidationError("amount must be a positive integer in minor units")
	}
	if req.Amount > constants.MaxAmount {
		return domain.NewValidationError("amount must not exceed %d minor units", constants.MaxAmount)
	}
	return nil
}

func validateWalletID(field, id string) error {
	if id == "" {
		return domain.NewValidationError("%s is required", field)
	}
	if len(id) > constants.MaxIDLength {
		return domain.NewValidationError("%s must be at most %d characters", field, constants.MaxIDLength)
	}
	if containsNUL(id) {
		return domain.NewValidationError("%s must not contain NUL bytes", field)
	}
	return nil
}

func validateKey(key string) error {
	if key == "" {
		return domain.NewValidationError("idempotencyKey is required")
	}
	if len(key) > constants.MaxKeyLength {
		return domain.NewValidationError("idempotencyKey must be at most %d characters", constants.MaxKeyLength)
	}
	if strings.TrimSpace(key) != key {
		// Leading or trailing whitespace is almost always a client bug, and
		// treating "k" and "k " as different keys would quietly create
		// duplicate transfers.
		return domain.NewValidationError("idempotencyKey must not have leading or trailing whitespace")
	}
	if containsNUL(key) {
		return domain.NewValidationError("idempotencyKey must not contain NUL bytes")
	}
	return nil
}

// containsNUL reports whether s contains a NUL byte. NUL is the one character
// PostgreSQL text and jsonb cannot carry (SQLSTATE 22021 / 22P05), so it is
// rejected as malformed client input (400) instead of surfacing a database
// error as a 500.
func containsNUL(s string) bool { return strings.IndexByte(s, 0) >= 0 }

// canonicalRequest renders the request identity that idempotency is keyed on.
// It is built from decoded values in a fixed field order, never from the raw
// client bytes, so that cosmetically different but semantically identical
// bodies compare equal - and semantically different bodies never do.
func canonicalRequest(req dto.TransferRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("canonicalize request: %w", err)
	}
	return body, nil
}
