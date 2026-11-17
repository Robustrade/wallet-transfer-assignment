package service

import (
	"errors"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

// Application/API outcome errors mapped by the HTTP handler.
// Domain validation errors live in model/errors.go.
var (
	ErrWalletNotFound      = errors.New("wallet_not_found")
	ErrTransferNotFound    = errors.New("transfer_not_found")
	ErrWalletAlreadyExists = errors.New("wallet id already exists, choose a different one")
	ErrIdempotencyMismatch = errors.New("idempotency_key_mismatch")
	ErrInvariantBroken     = errors.New("internal")
)

// ErrOpeningBalanceImmutable is re-exported for callers that only import service.
var ErrOpeningBalanceImmutable = model.ErrOpeningBalanceImmutable
