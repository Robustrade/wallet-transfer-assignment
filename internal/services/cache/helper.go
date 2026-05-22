package cache

// CacheHelper defines the interface for our Tier 1 idempotency store.
// Tier 1 is an in-memory high-speed cache that prevents identical concurrent requests
// from reaching the database (Tier 2).
type CacheHelper interface {
	Get(key string) (CacheItem, bool)
	Set(key string, item CacheItem)
	Delete(key string)
	// CompareAndSwapState provides an atomic mechanism to transition between states.
	// This is critical for preventing race conditions in high-concurrency bursts.
	CompareAndSwapState(key string, oldState, newState IdempotencyState) bool
	// Clear removes all entries from the cache.
	Clear()
}
