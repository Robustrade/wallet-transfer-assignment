package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// IdempotencyRecord claims a key and later stores the HTTP outcome.
type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	RequestBody    json.RawMessage
	ResponseCode   *int
	ResponseBody   json.RawMessage
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TransferRequest is the canonical business payload hashed for idempotency.
type TransferRequest struct {
	Amount       int64  `json:"amount"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
}

// CanonicalTransferJSON builds the UTF-8 canonical JSON used for request_hash.
// Keys are sorted lexicographically; amount is a JSON integer; no insignificant whitespace.
func CanonicalTransferJSON(fromWalletID, toWalletID string, amount int64) ([]byte, error) {
	// Explicit map + Marshal produces sorted keys for this fixed set.
	payload := map[string]any{
		"amount":       amount,
		"fromWalletId": fromWalletID,
		"toWalletId":   toWalletID,
	}
	return json.Marshal(payload)
}

// HashTransferRequest returns the lowercase hex SHA-256 of the canonical request JSON.
func HashTransferRequest(fromWalletID, toWalletID string, amount int64) (hash string, body []byte, err error) {
	body, err = CanonicalTransferJSON(fromWalletID, toWalletID, amount)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), body, nil
}

// NewIdempotencyRecord creates an in-flight claim (response fields nil).
func NewIdempotencyRecord(key string, requestHash string, requestBody []byte) (*IdempotencyRecord, error) {
	key, err := NormalizeIdempotencyKey(key)
	if err != nil {
		return nil, err
	}
	if requestHash == "" || len(requestBody) == 0 {
		return nil, ErrInvalidIdempotencyKey
	}
	now := time.Now().UTC()
	return &IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    requestHash,
		RequestBody:    append(json.RawMessage(nil), requestBody...),
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// StoreResponse fills the terminal HTTP outcome. Both code and body must be set together.
func (r *IdempotencyRecord) StoreResponse(code int, body []byte) error {
	if code == 0 || len(body) == 0 {
		return ErrInvalidTransition
	}
	if r.ResponseCode != nil {
		return ErrInvalidTransition
	}
	r.ResponseCode = &code
	r.ResponseBody = append(json.RawMessage(nil), body...)
	r.UpdatedAt = time.Now().UTC()
	return nil
}

// HasResponse reports whether the outcome has been stored.
func (r *IdempotencyRecord) HasResponse() bool {
	return r.ResponseCode != nil && r.ResponseBody != nil
}
