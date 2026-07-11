// Package idgen generates Snowflake-style, time-ordered 64-bit IDs used as
// transfer identifiers. A transfer's ID doubles as its idempotency key and
// as the foreign-key target for ledger_entries —
// nothing outside this package should ever need to decode or depend on the
// internal bit layout below.
package idgen

import (
	"fmt"
	"sync"
	"time"
)

const (
	// epoch is a custom reference point (2026-01-01T00:00:00Z in Unix ms),
	// not the Unix epoch — this keeps generated IDs from starting out at a
	// 1970-scale magnitude and buys ~69 years of headroom before the
	// timestamp component wraps around.
	epoch int64 = 1767225600000

	workerBits   uint8 = 10
	sequenceBits uint8 = 12

	workerShift    = sequenceBits
	timestampShift = workerBits + sequenceBits

	maxWorker   int64 = -1 ^ (-1 << workerBits)
	maxSequence int64 = -1 ^ (-1 << sequenceBits)
)

// Generator mints Snowflake IDs for a single worker (process/replica).
// It is safe for concurrent use.
type Generator struct {
	mu sync.Mutex

	workerID int64

	lastTimestamp int64
	sequence      int64
}

// NewGenerator constructs a Generator for the given workerID.
//
// workerID must be unique across every concurrently running instance of
// this service — two instances sharing a workerID can produce colliding
// IDs. For this project, workerID comes from config (WORKER_ID env var,
// default 0 for a single instance).
func NewGenerator(workerID int64) (*Generator, error) {
	if workerID < 0 || workerID > maxWorker {
		return nil, fmt.Errorf("idgen: workerID must be between 0 and %d, got %d", maxWorker, workerID)
	}
	return &Generator{workerID: workerID}, nil
}

// nowMillis is a variable (not a plain function call) so tests can
// substitute a controllable clock without changing NextID's logic.
var nowMillis = func() int64 {
	return time.Now().UnixMilli()
}

// NextID returns a new, monotonically increasing (within this Generator)
// 64-bit ID, laid out as:
//
//	[ 41 bits: ms since epoch ][ 10 bits: workerID ][ 12 bits: sequence ]
//
// Up to 4096 IDs can be minted per worker per millisecond; if that budget
// is exhausted, NextID blocks until the next millisecond rather than
// reusing a sequence value.
//
// If the system clock has moved backward since the last call, NextID
// returns an error instead of spinning — a regression can be seconds or
// minutes long (NTP step, VM migration), and blocking the caller for
// that long would be worse than failing fast.
func (g *Generator) NextID() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := nowMillis()

	if now < g.lastTimestamp {
		return 0, fmt.Errorf("idgen: clock moved backward by %dms, refusing to generate id", g.lastTimestamp-now)
	}

	if now == g.lastTimestamp {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			// Sequence space exhausted for this millisecond — spin until
			// the clock ticks forward rather than reuse a sequence value,
			// which would risk a duplicate ID within the same worker.
			for now <= g.lastTimestamp {
				now = nowMillis()
			}
		}
	} else {
		g.sequence = 0
	}

	g.lastTimestamp = now

	return (now-epoch)<<timestampShift | g.workerID<<workerShift | g.sequence, nil
}
