package transfer

import (
	"errors"
	"strings"

	"wallet-transfer-assignment/internal/models"
)

// ValidationError represents a client-side validation failure (HTTP 400).
// These errors indicate the request payload itself is invalid (e.g., negative amounts,
// self-transfers, or non-existent wallet IDs).
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// BusinessError represents a valid request that failed domain-specific rules (HTTP 422).
// These are used for scenarios like "insufficient funds" where the request is well-formed
// but cannot be fulfilled due to the current system state.
type BusinessError struct {
	Message string
}

func (e *BusinessError) Error() string {
	return e.Message
}

// IsValidationError determines if an error should be treated as a client-side validation failure.
// It checks for explicit ValidationError types and repository-layer "not found" errors.
func IsValidationError(err error) bool {
	if err == nil {
		return false
	}
	var valErr *ValidationError
	if errors.As(err, &valErr) {
		return true
	}

	// Repository "not found" errors are treated as validation errors (HTTP 400)
	// because they indicate the client provided a non-existent Resource ID.
	if strings.Contains(strings.ToLower(err.Error()), "not found") {
		return true
	}

	return false
}

// IsBusinessError determines if an error represents a business rule violation.
// Primarily used to trigger 422 Unprocessable Entity responses for balance issues.
func IsBusinessError(err error) bool {
	if err == nil {
		return false
	}
	var busErr *BusinessError
	if errors.As(err, &busErr) {
		return true
	}

	// Fallback check for business-level error strings if they haven't been explicitly typed yet.
	if strings.Contains(strings.ToLower(err.Error()), "insufficient funds") {
		return true
	}

	return false
}

// validateDoWalletTransfer performs pre-transactional sanity checks.
// This prevents unnecessary database locks for obviously invalid requests.
func validateDoWalletTransfer(transfer *models.Transfer) error {
	// Constraint: Transfers must be between 100 and 1,000,000 units.
	if transfer.Amount < 100 {
		return &ValidationError{Message: "transfer amount must be greater than or equal to 100"}
	}
	if transfer.Amount > 1000000 {
		return &ValidationError{Message: "transfer amount must be less than or equal to 1,000,000"}
	}

	// Constraint: A wallet cannot transfer to itself.
	if transfer.SourceWalletID == transfer.DestWalletID {
		return &ValidationError{Message: "source and destination wallets must be different"}
	}

	// Constraint: Required fields check.
	if transfer.SourceWalletID == "" || transfer.DestWalletID == "" {
		return &ValidationError{Message: "wallet IDs cannot be empty"}
	}
	return nil
}
