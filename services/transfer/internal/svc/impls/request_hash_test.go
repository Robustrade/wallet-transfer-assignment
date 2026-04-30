package svcimpl

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
)

// TestHashRequest_DistinguishesDelimiterCollisions guards against the naive
// concatenation bug where ("a|b","c") and ("a","b|c") with the same amount
// would produce the same hash. Length-prefixed encoding prevents that.
func TestHashRequest_DistinguishesDelimiterCollisions(t *testing.T) {
	a := request.CreateTransferReq{
		IdempotencyKey: "k",
		FromWalletID:   "a|b",
		ToWalletID:     "c",
		Amount:         100,
	}
	b := request.CreateTransferReq{
		IdempotencyKey: "k",
		FromWalletID:   "a",
		ToWalletID:     "b|c",
		Amount:         100,
	}
	assert.NotEqual(t, hashRequest(a), hashRequest(b),
		"hash must distinguish payloads that share a flat concatenation",
	)
}

// TestHashRequest_StableForSameInput sanity-checks determinism.
func TestHashRequest_StableForSameInput(t *testing.T) {
	req := request.CreateTransferReq{
		IdempotencyKey: "k",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	assert.Equal(t, hashRequest(req), hashRequest(req))
}

// TestHashRequest_IgnoresIdempotencyKey ensures only the body fields
// participate in the hash — the idempotency key is the *key*, not part of the
// body identity.
func TestHashRequest_IgnoresIdempotencyKey(t *testing.T) {
	a := request.CreateTransferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	b := a
	b.IdempotencyKey = "k2"
	assert.Equal(t, hashRequest(a), hashRequest(b))
}

// TestHashRequest_DetectsAmountChange ensures amount is part of the hash.
func TestHashRequest_DetectsAmountChange(t *testing.T) {
	a := request.CreateTransferReq{
		IdempotencyKey: "k",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
	b := a
	b.Amount = 101
	assert.NotEqual(t, hashRequest(a), hashRequest(b))
}
