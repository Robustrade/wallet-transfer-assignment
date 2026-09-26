// Package constants centralizes runtime values shared across the wallet service.
package constants

import (
	"time"

	"wallet/internal/domain"
)

// Input limits. MaxAmount keeps credit arithmetic far away from int64
// overflow: the debit guard clause (balance >= amount) bounds how much can be
// deducted, and no realistic balance plus 10^15 approaches 2^63-1.
const (
	MaxAmount    int64 = 1_000_000_000_000_000 // 10^15 minor units
	MaxIDLength        = 255
	MaxKeyLength       = 255
)

// PENDING -> PROCESSED | FAILED. Terminal states never change.
const (
	TransferPending   domain.TransferStatus = "PENDING"
	TransferProcessed domain.TransferStatus = "PROCESSED"
	TransferFailed    domain.TransferStatus = "FAILED"
)

// Failure reasons recorded on FAILED transfers.
const (
	FailureInsufficientFunds = "INSUFFICIENT_FUNDS"
	FailureBalanceOverflow   = "BALANCE_OVERFLOW"
)

const (
	EntryDebit  domain.EntryType = "DEBIT"
	EntryCredit domain.EntryType = "CREDIT"
)

// Error codes used in error responses.
const (
	CodeValidation          = "VALIDATION_ERROR"
	CodeWalletNotFound      = "WALLET_NOT_FOUND"
	CodeTransferNotFound    = "TRANSFER_NOT_FOUND"
	CodeInsufficientFunds   = "INSUFFICIENT_FUNDS"
	CodeBalanceOverflow     = "BALANCE_OVERFLOW"
	CodeKeyReused           = "IDEMPOTENCY_KEY_REUSED"
	CodeLockTimeout         = "LOCK_TIMEOUT"
	CodeStatementTimeout    = "STATEMENT_TIMEOUT"
	CodeDatabaseUnavailable = "DATABASE_UNAVAILABLE"
	CodeOutcomeUnknown      = "OUTCOME_UNKNOWN"
	CodeClientClosed        = "CLIENT_CLOSED_REQUEST"
	CodeNotFound            = "NOT_FOUND"
	CodeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	CodeInternal            = "INTERNAL_ERROR"
)

// MaxConns bounds the pool per instance. Measured (README, "Pool sizing"):
// read and replay throughput scales up to 32 connections and plateaus there,
// while transfers are serialized by the wallet row lock and gain nothing from
// more connections. Keep instances*MaxConns below the server's
// max_connections.
const MaxConns int32 = 32

// ConnectTimeout bounds database connection establishment and pool acquisition
// so commands and requests fail fast on an unreachable or saturated database.
const ConnectTimeout = 5 * time.Second

const (
	ShutdownTimeout   = 10 * time.Second
	ReadHeaderTimeout = 5 * time.Second
	ReadTimeout       = 15 * time.Second
	WriteTimeout      = 30 * time.Second
	IdleTimeout       = 60 * time.Second
)

// StatementTimeoutMS bounds how long any single transfer-transaction statement
// may run. The transaction's statements are all short index/row operations; a
// statement that runs longer than this is a symptom (lock pile-up, I/O stall),
// and failing fast beats holding the connection.
const StatementTimeoutMS = 5000

// AdvisoryLockKey is an arbitrary constant unique to this application; it
// serializes concurrent migration runners.
const AdvisoryLockKey int64 = 0x77616c6c6574 // "wallet"

// MigrationLockWait bounds how long a runner waits for the advisory lock. A
// healthy run holds the lock for milliseconds; waiting a full minute means
// another runner is stuck, and failing loudly beats hanging forever.
const MigrationLockWait = time.Minute

// MaxBodyBytes bounds request bodies. Transfer requests are tiny; anything
// larger is a mistake or an attack.
const MaxBodyBytes = 64 << 10

const HealthTimeout = 2 * time.Second

// ClientClosedRequest is the conventional (non-standard) status for a client
// that disconnected before the response could be delivered.
const ClientClosedRequest = 499

// MaxLedgerLimit caps the ledger listing endpoint.
const MaxLedgerLimit = 100
