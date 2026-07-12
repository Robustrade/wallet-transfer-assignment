// Package redisclient bootstraps the Redis client. It has no domain
// knowledge — doesn't know what a "wallet" is. Callers are responsible
// for pinging the returned client before relying on it.
package redisclient

import "github.com/redis/go-redis/v9"

// New constructs a client for addr. It does not verify connectivity —
// call Ping on the result to fail fast.
func New(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}
