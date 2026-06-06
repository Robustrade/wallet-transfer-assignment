package domain

import "github.com/google/uuid"

// NewID generates a new UUID version 7 identifier.
// UUID v7 is time-ordered, which provides natural chronological sorting in the
// database without additional indexed timestamp columns.
func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// NewV7 reads from crypto/rand; failure is unrecoverable in practice.
		panic("domain: failed to generate UUID v7: " + err.Error())
	}

	return id
}

// ZeroID returns the nil UUID used as an unset/zero sentinel value.
func ZeroID() uuid.UUID {
	return uuid.UUID{}
}
