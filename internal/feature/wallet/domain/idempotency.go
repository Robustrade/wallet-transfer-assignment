package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// Idempotency key length limits. The minimum matches the database CHECK
// (chk_idempotency_key_length); the maximum matches VARCHAR(64).
const (
	MinIdempotencyKeyLength = 8
	MaxIdempotencyKeyLength = 64
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key already used with different request")
	ErrIdempotencyNotFound = errors.New("idempotency record not found")
	ErrInvalidIdempotency  = errors.New("invalid idempotency record")
)

type IdempotencyRecord struct {
	ID             int64
	IdempotencyKey string
	RequestHash    string
	TransferID     *int64
	ResponseStatus *int
	ResponseBody   json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TransferFingerprint generates a deterministic hash of transfer details.
func TransferFingerprint(fromWalletID, toWalletID, amount int64) (string, error) {
	payload := struct {
		FromWalletID int64 `json:"fromWalletId"`
		ToWalletID   int64 `json:"toWalletId"`
		Amount       int64 `json:"amount"`
	}{
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)

	return hex.EncodeToString(hash[:]), nil
}

// MatchesRequest checks whether the same key is being reused
// with the same transfer details.
func (r IdempotencyRecord) MatchesRequest(hash string) error {
	if r.RequestHash != hash {
		return ErrIdempotencyConflict
	}

	return nil
}
