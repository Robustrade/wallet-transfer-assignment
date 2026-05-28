package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sync/singleflight"
)

type Key string

type StoredResponse struct {
	RequestHash string
	StatusCode  int
	Body        []byte
}

type Record struct {
	Key         Key
	RequestHash string
	StatusCode  int
	Body        json.RawMessage
	Ready       bool
}

type Store interface {
	// TryInsert creates a row for key+hash if it doesn't exist. If it exists, it returns the stored record.
	// Implementations must be safe under concurrent inserts (unique constraint).
	TryInsert(ctx context.Context, tx Tx, key Key, requestHash string) (inserted bool, existing Record, err error)
	Get(ctx context.Context, key Key) (Record, error)
	SetResponse(ctx context.Context, tx Tx, key Key, requestHash string, status int, body json.RawMessage) error
}

// Tx is the minimal interface we need from a DB transaction.
type Tx interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

type Row interface {
	Scan(dest ...any) error
}

type PgxTxAdapter struct {
	Inner pgx.Tx
}

func AdaptPgxTx(tx pgx.Tx) Tx {
	return PgxTxAdapter{Inner: tx}
}

func (a PgxTxAdapter) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return a.Inner.Exec(ctx, sql, arguments...)
}

func (a PgxTxAdapter) QueryRow(ctx context.Context, sql string, args ...any) Row {
	return a.Inner.QueryRow(ctx, sql, args...)
}

func HashTransfer(fromWalletID, toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amount)))
	return hex.EncodeToString(sum[:])
}

type Cache[K comparable, V any] interface {
	Get(K) (V, bool)
	Set(K, V, time.Duration)
}

type CachedStore struct {
	inner Store
	cache Cache[Key, StoredResponse]
	ttl   time.Duration
	sf    singleflight.Group
}

func NewCachedStore(inner Store, cache Cache[Key, StoredResponse]) *CachedStore {
	return &CachedStore{
		inner: inner,
		cache: cache,
		ttl:   10 * time.Minute,
	}
}

func (s *CachedStore) Get(ctx context.Context, key Key) (Record, error) {
	v, err, _ := s.sf.Do(string(key), func() (any, error) {
		if cached, ok := s.cache.Get(key); ok {
			return Record{
				Key:         key,
				RequestHash: cached.RequestHash,
				StatusCode:  cached.StatusCode,
				Body:        cached.Body,
				Ready:       true,
			}, nil
		}

		rec, err := s.inner.Get(ctx, key)
		if err != nil {
			return Record{}, err
		}
		if rec.Ready {
			s.cache.Set(key, StoredResponse{
				RequestHash: rec.RequestHash,
				StatusCode:  rec.StatusCode,
				Body:        rec.Body,
			}, s.ttl)
		}
		return rec, nil
	})
	if err != nil {
		return Record{}, err
	}
	return v.(Record), nil
}

func (s *CachedStore) TryInsert(ctx context.Context, tx Tx, key Key, requestHash string) (bool, Record, error) {
	inserted, existing, err := s.inner.TryInsert(ctx, tx, key, requestHash)
	if err != nil {
		return false, Record{}, err
	}

	// If we found an existing finalized record, cache it.
	if !inserted && existing.Ready {
		s.cache.Set(key, StoredResponse{
			RequestHash: existing.RequestHash,
			StatusCode:  existing.StatusCode,
			Body:        existing.Body,
		}, s.ttl)
	}
	return inserted, existing, nil
}

func (s *CachedStore) SetResponse(ctx context.Context, tx Tx, key Key, requestHash string, status int, body json.RawMessage) error {
	return s.inner.SetResponse(ctx, tx, key, requestHash, status, body)
}

// CacheFinalizedResponse stores a committed idempotency response into the in-memory cache.
// Call this only after the surrounding DB transaction successfully commits.
func (s *CachedStore) CacheFinalizedResponse(key Key, requestHash string, status int, body json.RawMessage) {
	s.cache.Set(key, StoredResponse{
		RequestHash: requestHash,
		StatusCode:  status,
		Body:        body,
	}, s.ttl)
}
