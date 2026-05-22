package utils

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// IsUniqueConstraintViolation checks if the given database error is a unique constraint violation.
// It handles both standard GORM duplicate key errors and native PostgreSQL "23505" unique violation codes.
// This utility ensures consistent error handling across the repository layer.
func IsUniqueConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return errors.Is(err, gorm.ErrDuplicatedKey) ||
		strings.Contains(errStr, "23505") ||
		strings.Contains(errStr, "duplicate key")
}
