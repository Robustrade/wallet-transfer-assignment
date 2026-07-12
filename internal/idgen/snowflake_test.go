package idgen

import (
	"sync"
	"testing"
)

func TestNewGenerator_RejectsInvalidWorkerID(t *testing.T) {
	if _, err := NewGenerator(-1); err == nil {
		t.Error("expected error for negative workerID, got nil")
	}
	if _, err := NewGenerator(maxWorker + 1); err == nil {
		t.Error("expected error for workerID above max, got nil")
	}
	if _, err := NewGenerator(0); err != nil {
		t.Errorf("expected workerID 0 to be valid, got error: %v", err)
	}
	if _, err := NewGenerator(maxWorker); err != nil {
		t.Errorf("expected max workerID to be valid, got error: %v", err)
	}
}

func TestNextID_MonotonicallyIncreasing(t *testing.T) {
	g, err := NewGenerator(1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var prev int64
	for i := 0; i < 10_000; i++ {
		id, err := g.NextID()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id <= prev {
			t.Fatalf("expected strictly increasing IDs, got %d after %d", id, prev)
		}
		prev = id
	}
}

func TestNextID_UniqueUnderConcurrency(t *testing.T) {
	g, err := NewGenerator(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const goroutines = 50
	const perGoroutine = 2_000

	ids := make(chan int64, goroutines*perGoroutine)
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				id, err := g.NextID()
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)

	seen := make(map[int64]struct{}, goroutines*perGoroutine)
	for id := range ids {
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate ID generated: %d", id)
		}
		seen[id] = struct{}{}
	}
}

func TestNextID_DifferentWorkersNeverCollide(t *testing.T) {
	g1, err := NewGenerator(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	g2, err := NewGenerator(4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Freeze both generators to the same instant (after the package's
	// custom epoch) so this test actually exercises the worker-ID bit
	// separation, not just the timestamp component happening to differ
	// across calls.
	frozen := epoch + 1_000
	restore := nowMillis
	nowMillis = func() int64 { return frozen }
	defer func() { nowMillis = restore }()

	id1, err := g1.NextID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id2, err := g2.NextID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id1 == id2 {
		t.Fatalf("expected different workers to produce different IDs at the same instant, got %d for both", id1)
	}
}

func TestNextID_SequenceRolloverAdvancesTimestamp(t *testing.T) {
	g, err := NewGenerator(5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	frozen := epoch + 2_000
	tickAfter := maxSequence + 1 // force rollover on this call
	calls := int64(0)

	restore := nowMillis
	nowMillis = func() int64 {
		calls++
		if calls > tickAfter {
			return frozen + 1
		}
		return frozen
	}
	defer func() { nowMillis = restore }()

	var last int64
	for i := int64(0); i <= tickAfter; i++ {
		id, err := g.NextID()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last = id
	}

	if last <= 0 {
		t.Fatalf("expected a valid ID after rollover, got %d", last)
	}
}

func TestNextID_ClockRegressionReturnsError(t *testing.T) {
	g, err := NewGenerator(6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	restore := nowMillis
	defer func() { nowMillis = restore }()

	frozen := epoch + 10_000
	nowMillis = func() int64 { return frozen }
	if _, err := g.NextID(); err != nil {
		t.Fatalf("unexpected error priming the generator: %v", err)
	}

	nowMillis = func() int64 { return frozen - 1_000 }
	if _, err := g.NextID(); err == nil {
		t.Fatal("expected an error when the clock moves backward, got nil")
	}
}
