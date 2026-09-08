// Package id provides collision-resistant identifiers for domain aggregates.
package id

import (
	"crypto/rand"
	"encoding/hex"
)

// Generator produces prefixed random identifiers, e.g. "txn_9f2c...".
type Generator struct {
	prefix string
	size   int
}

// NewGenerator returns a Generator emitting ids of the form "<prefix>_<hex>".
// size is the number of random bytes (the hex string is twice as long).
func NewGenerator(prefix string, size int) *Generator {
	if size <= 0 {
		size = 16
	}
	return &Generator{prefix: prefix, size: size}
}

// NewID returns a fresh identifier. It panics only if the system CSPRNG fails,
// which is not a recoverable condition.
func (g *Generator) NewID() string {
	buf := make([]byte, g.size)
	if _, err := rand.Read(buf); err != nil {
		panic("id: crypto/rand failed: " + err.Error())
	}
	return g.prefix + "_" + hex.EncodeToString(buf)
}
