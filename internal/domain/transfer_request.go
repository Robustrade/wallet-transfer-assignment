package domain

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxFieldLength matches the VARCHAR(255) columns, which count characters,
// not bytes.
const maxFieldLength = 255

// TransferRequest is a validated request to move Amount from one wallet to
// another.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// NewTransferRequest validates the request fields in the order of API spec §6
// steps 3–5. amount is the raw JSON text of the amount field. A missing or
// null field is passed as "".
func NewTransferRequest(idempotencyKey, fromWalletID, toWalletID, amount string) (TransferRequest, error) {
	fields := []struct{ name, value string }{
		{"idempotencyKey", idempotencyKey},
		{"fromWalletId", fromWalletID},
		{"toWalletId", toWalletID},
	}
	for _, f := range fields {
		if n := utf8.RuneCountInString(f.value); n < 1 || n > maxFieldLength {
			return TransferRequest{}, fmt.Errorf("%w: %s must be 1 to %d characters", ErrInvalidField, f.name, maxFieldLength)
		}
		// Postgres text cannot store NUL, so reject it here rather than fail
		// in the database.
		if strings.ContainsRune(f.value, 0) {
			return TransferRequest{}, fmt.Errorf("%w: %s must not contain U+0000", ErrInvalidField, f.name)
		}
	}
	if amount == "" {
		return TransferRequest{}, fmt.Errorf("%w: amount is required", ErrInvalidField)
	}

	// ParseInt rejects "100" in quotes, 100.5, 100.0, 1e2, and values beyond
	// int64, so only a JSON integer gets through.
	parsed, err := strconv.ParseInt(amount, 10, 64)
	if err != nil || parsed <= 0 {
		return TransferRequest{}, ErrInvalidAmount
	}

	if fromWalletID == toWalletID {
		return TransferRequest{}, ErrSameWallet
	}

	return TransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         parsed,
	}, nil
}
