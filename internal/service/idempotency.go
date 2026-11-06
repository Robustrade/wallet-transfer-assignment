package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// Idempotency handling (docs/DESIGN.md §5). The key is claimed inside the
// transfer's own transaction, so the claim commits or rolls back together with
// the money movement: a committed key always points at a finished transfer.

// claimKey claims req's idempotency key for transferID and reports whether it did.
// When the key was already used, it returns the stored outcome to replay instead,
// or domain.ErrIdempotencyKeyReused if that key was used for a different request.
// A concurrent claim of the same key blocks until the other transaction ends.
func claimKey(ctx context.Context, tx Store, req domain.TransferRequest, transferID uuid.UUID) (bool, CreateTransferResult, error) {
	hash := fingerprint(req)
	claimed, err := tx.Idempotency().Claim(ctx, IdempotencyRecord{
		Key: req.IdempotencyKey, RequestHash: hash, TransferID: transferID,
	})
	if err != nil || claimed {
		return claimed, CreateTransferResult{}, err
	}
	prior, err := replay(ctx, tx, req.IdempotencyKey, hash)
	return false, prior, err
}

// replay returns the stored outcome for an already-used key, provided the request matches.
func replay(ctx context.Context, tx Store, key, hash string) (CreateTransferResult, error) {
	rec, err := tx.Idempotency().Get(ctx, key)
	if err != nil {
		return CreateTransferResult{}, err
	}
	if rec.RequestHash != hash {
		return CreateTransferResult{}, domain.ErrIdempotencyKeyReused
	}
	transfer, err := tx.Transfers().Get(ctx, rec.TransferID)
	if err != nil {
		return CreateTransferResult{}, fmt.Errorf("load transfer for replayed key: %w", err)
	}
	return CreateTransferResult{Transfer: transfer, Replayed: true}, nil
}

// fingerprint binds an idempotency key to the request content it was first used with.
func fingerprint(req domain.TransferRequest) string {
	// json.Marshal of a fixed struct is deterministic and unambiguous (fields are quoted).
	payload, _ := json.Marshal(struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}{req.FromWalletID, req.ToWalletID, req.Amount})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
