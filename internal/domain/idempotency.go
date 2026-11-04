package domain

type IdempotencyStatus string

const (
	IdempotencyStatusProcessing      IdempotencyStatus = "PROCESSING"
	IdempotencyStatusRetryableFailed IdempotencyStatus = "RETRYABLE_FAILED"
	IdempotencyStatusCompleted       IdempotencyStatus = "COMPLETED"
)

type IdempotencyFailureReason string

const (
	IdempotencyFailureInsufficientFunds IdempotencyFailureReason = "INSUFFICIENT_FUNDS"
)
