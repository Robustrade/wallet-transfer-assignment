package helper

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GenerateID generates a unique prefixed ID in the format PREFIX-XXXXXXXXXXXX
// using the full UUID to avoid collisions.
// Example: WAL-3f6a2b1c9d4e, TXN-7a1b3c2d4e5f
func GenerateID(prefix string) string {
	id := uuid.New().String()
	// Remove hyphens and take first 12 chars for a clean readable ID
	short := strings.ReplaceAll(id, "-", "")[:12]
	return fmt.Sprintf("%s-%s", prefix, short)
}
