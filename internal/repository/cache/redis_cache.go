// Package cache implements a transfer:{id} read-through cache. It is a
// latency optimization only — the Postgres unique constraint on
// transfers.id is the actual correctness guarantee; this cache could
// disappear entirely and idempotency would still hold.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
)

const (
	keyPrefix = "transfer:"
	baseTTL   = 24 * time.Hour
	jitterMax = 30 * time.Minute
)

// TransferCache caches domain.Transfer rows by ID. A transfers row is
// immutable after creation, so there is no update path to maintain —
// only Get and a single Set, written once per ID.
type TransferCache struct {
	client *redis.Client
}

// NewTransferCache constructs a TransferCache.
func NewTransferCache(client *redis.Client) *TransferCache {
	return &TransferCache{client: client}
}

// Get returns the cached transfer for id, or (nil, nil) on a cache miss.
func (c *TransferCache) Get(ctx context.Context, id int64) (*domain.Transfer, error) {
	raw, err := c.client.Get(ctx, key(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cached transfer: %w", err)
	}

	var transfer domain.Transfer
	if err := json.Unmarshal(raw, &transfer); err != nil {
		return nil, fmt.Errorf("decode cached transfer: %w", err)
	}
	return &transfer, nil
}

// Set caches transfer under its ID with a base TTL plus jitter, to avoid
// many keys written around the same time all expiring together.
func (c *TransferCache) Set(ctx context.Context, id int64, transfer *domain.Transfer) error {
	raw, err := json.Marshal(transfer)
	if err != nil {
		return fmt.Errorf("encode transfer for cache: %w", err)
	}

	ttl := baseTTL + time.Duration(rand.Int64N(int64(jitterMax)))
	if err := c.client.Set(ctx, key(id), raw, ttl).Err(); err != nil {
		return fmt.Errorf("set cached transfer: %w", err)
	}
	return nil
}

func key(id int64) string {
	return keyPrefix + strconv.FormatInt(id, 10)
}
