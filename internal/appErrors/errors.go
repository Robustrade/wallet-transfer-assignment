// Package appErrors defines the application's shared errors without depending on
// configuration, persistence, service, or HTTP packages.
package appErrors

import (
	"errors"
	"fmt"
)

var (
	// Configuration.
	ErrDatabaseURLRequired      = errors.New("DATABASE_URL is required")
	ErrInvalidPort              = errors.New("HTTP_PORT must be an integer between 1 and 65535")
	ErrInvalidShutdownTimeout   = errors.New("HTTP_SHUTDOWN_TIMEOUT must be a positive duration")
	ErrInvalidOperationTimeout  = errors.New("DB_OPERATION_TIMEOUT must be a positive duration")
	ErrInvalidMaxTransferAmount = errors.New("MAX_TRANSFER_AMOUNT must be a positive int64")

	// Transfer rules and retryable outcomes.
	ErrInvalidTransfer                = errors.New("invalid transfer")
	ErrInvalidTransition              = errors.New("invalid transfer state transition")
	ErrWalletNotFound                 = errors.New("wallet not found")
	ErrTransferNotFound               = errors.New("transfer not found")
	ErrBalanceOverflow                = errors.New("destination balance would exceed int64")
	ErrIdempotencyConflict            = errors.New("idempotency key reused for another payload")
	ErrInsufficientFunds              = errors.New("insufficient funds")
	ErrServiceUnavailable             = errors.New("service unavailable")
	ErrTransferTemporarilyUnavailable = errors.New("transfer temporarily unavailable")

	// Persistence.
	ErrDatabaseOpen            = errors.New("database configuration rejected")
	ErrDatabaseConnection      = errors.New("database connection failed")
	ErrSchemaUnavailable       = errors.New("schema migration metadata unavailable")
	ErrSchemaIncompatible      = errors.New("schema migration version incompatible or dirty")
	ErrUnexpectedRowCount      = errors.New("database operation affected an unexpected number of rows")
	ErrTerminalResponseMissing = errors.New("terminal transfer has no valid saved response")

	// Server lifecycle.
	ErrListen   = errors.New("HTTP listener could not start")
	ErrServe    = errors.New("HTTP server failed")
	ErrShutdown = errors.New("HTTP shutdown grace period expired or shutdown failed")
)

type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string { return fmt.Sprintf("%s: %s", e.Field, e.Reason) }
func (e *ValidationError) Unwrap() error { return ErrInvalidTransfer }
