package cache

import (
	"sync"
	"time"
)

// CacheItem represents a single entry in the idempotency cache.
// It stores the lifecycle state and the eventual final response for deduplication.
type CacheItem struct {
	State      IdempotencyState // Current lifecycle phase (ISSUED, IN_FLIGHT, COMPLETED)
	StatusCode int              // The HTTP status code to return for cached hits
	Response   []byte           // The serialized JSON response body
	CreatedAt  time.Time        // Used for TTL cleanup
}

// syncMapCache is a thread-safe, in-memory implementation of the CacheHelper interface.
type syncMapCache struct {
	data sync.Map      // Stores CacheItem by string key
	ttl  time.Duration // Time-to-live for cache entries
	// mu protects the atomicity of state transitions for a specific key.
	// While sync.Map handles parallel reads/writes, it doesn't support atomic
	// "check-and-update" logic across multiple calls without external locking.
	mu sync.Mutex
}

// NewSyncMapCache creates a new in-memory cache and starts a background cleanup worker.
func NewSyncMapCache(ttl time.Duration) CacheHelper {
	c := &syncMapCache{
		ttl: ttl,
	}
	go c.cleanupLoop()
	return c
}

func (c *syncMapCache) Get(key string) (CacheItem, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return CacheItem{}, false
	}
	return val.(CacheItem), true
}

func (c *syncMapCache) Set(key string, item CacheItem) {
	c.data.Store(key, item)
}

func (c *syncMapCache) Delete(key string) {
	c.data.Delete(key)
}

// CompareAndSwapState ensures only one goroutine can transition a key from 'ISSUED' to 'IN_FLIGHT'.
// If two requests with the same key arrive at the exact same time, this mutex-guaranteed
// check prevents them both from executing the transfer logic.
func (c *syncMapCache) CompareAndSwapState(key string, oldState, newState IdempotencyState) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	val, ok := c.data.Load(key)
	if !ok {
		return false
	}

	item := val.(CacheItem)
	if item.State == oldState {
		item.State = newState
		c.data.Store(key, item)
		return true
	}

	return false
}

func (c *syncMapCache) Clear() {
	c.data.Clear()
}

// cleanupLoop periodically removes expired entries to prevent memory exhaustion.
func (c *syncMapCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		c.data.Range(func(key, value interface{}) bool {
			item := value.(CacheItem)
			if time.Since(item.CreatedAt) > c.ttl {
				c.data.Delete(key)
			}
			return true
		})
	}
}
