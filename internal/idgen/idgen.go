// Package idgen generates opaque, prefixed random identifiers without
// pulling in an external UUID dependency.
package idgen

import (
	"crypto/rand"
	"encoding/hex"
)

// New returns a random identifier like "txn_3f9a2b...". Collisions are
// astronomically unlikely (16 random bytes = 128 bits of entropy), which is
// the same margin a v4 UUID gives you.
func New(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only fails if the OS RNG is broken, which is a
		// fatal environment problem, not something callers can recover from.
		panic("idgen: failed to read random bytes: " + err.Error())
	}
	return prefix + "_" + hex.EncodeToString(b)
}
