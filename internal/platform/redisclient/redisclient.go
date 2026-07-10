// Package redisclient bootstraps the Redis client. It has no domain
// knowledge — see docs/DESIGN.md §8. Callers are responsible for pinging
// the returned client before relying on it.
package redisclient

import "github.com/redis/go-redis/v9"

// New constructs a client for addr. It does not verify connectivity —
// call Ping on the result to fail fast.
func New(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}
