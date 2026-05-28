---
name: wallet-integration-test
description: Use this skill when writing or modifying integration tests for the wallet-transfer service — that is, tests that hit a real PostgreSQL database (provided by docker-compose locally or the CI postgres service container) rather than mocks. Trigger when the user asks to "add a test", "test concurrency", "test idempotency", "test the HTTP endpoint", "add an integration test", "increase coverage", or whenever a new service/handler method needs corresponding test coverage. Trigger especially when editing files under internal/service/ or internal/handler/ that introduce a new code path. Do NOT trigger for pure unit tests on internal/domain/ (those are plain table-driven Go tests, no DB needed) or for documentation-only edits.
---

# wallet-integration-test

This repo runs integration tests against a real Postgres instance.
Pure-Go mocks for the DB are explicitly NOT used because the entire
contract under test — locks, transactions, constraint enforcement, race
conditions — is the database's behaviour. A mock would silently pass
tests that fail in production.

Read [AGENTS.md](../../../AGENTS.md) §6 (Testing rules) before writing.

---

## How the test harness works

`internal/testdb/testdb.go` exposes three functions:

```go
testdb.Get(t)      // *sql.DB pointing at a migrated database.
                   // Opens it lazily on first call from $DATABASE_URL;
                   // reused across all tests in the same package.
                   // Skips the test if $DATABASE_URL is unset.

testdb.DSN(t)      // The same DSN, for tests that need to construct
                   // their own *sql.DB (e.g. cmd/server smoke tests).

testdb.Reset(t)    // TRUNCATE every table. Call from the start of any
                   // test that mutates data.
```

The connection persists for the lifetime of the `go test` process. The
Postgres itself is provided externally:
- **Locally:** `make db-up` (docker-compose), then export
  `DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable`.
- **CI:** the `services.postgres` block in `.github/workflows/ci.yml`
  starts `postgres:16-alpine` and the integration step sets
  `DATABASE_URL` automatically.

Run with `make test-int` (which passes `-p 1` to serialise test binaries
across packages — they all share the one Postgres, so cross-package
parallelism would let one package `TRUNCATE` mid-test in another).

---

## Required scaffolding for every new integration test file

```go
//go:build integration

package <pkg>_test

import (
    "context"
    "testing"

    "github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
    // ... package under test
)
```

The `//go:build integration` tag is mandatory. Tests in this repo split
into two layers:
- `make test` runs only files without the tag.
- `make test-int` runs files with `-tags=integration`.

---

## Pattern A — service-level test

```go
func TestCreateTransfer_HappyPath(t *testing.T) {
    testdb.Reset(t)
    tsvc, wsvc := newTransferSvc(t)   // helper in transfer_integration_test.go
    ctx := context.Background()
    seedWallets(t, wsvc, map[string]int64{"w1": 500, "w2": 100})

    tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
        IdempotencyKey: "k1",
        FromWalletID:   "w1",
        ToWalletID:     "w2",
        Amount:         150,
    })
    if err != nil {
        t.Fatalf("create: %v", err)
    }
    if tr.State != domain.StateProcessed {
        t.Fatalf("state: got %s want PROCESSED", tr.State)
    }

    src, _ := wsvc.Get(ctx, "w1")
    if src.Balance != 350 {
        t.Fatalf("src balance: got %d want 350", src.Balance)
    }
}
```

Required steps:
1. `testdb.Reset(t)` — first line.
2. Build services via the existing helper.
3. Seed fixtures with `seedWallets`.
4. Exercise the system under test.
5. Assert observable behaviour (balances, states, ledger rows) — not
   implementation details (which query ran, which method was called).

---

## Pattern B — HTTP handler test

```go
func TestHTTP_TransferConflict(t *testing.T) {
    testdb.Reset(t)
    srv := newServer(t)   // helper that spins up httptest.NewServer with the real stack

    mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w1", "balance": 500}))
    mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w2", "balance": 0}))

    mustOK(t, postJSON(t, srv.URL+"/transfers", map[string]any{
        "idempotencyKey": "k1", "fromWalletId": "w1", "toWalletId": "w2", "amount": 50,
    }))
    resp := postJSON(t, srv.URL+"/transfers", map[string]any{
        "idempotencyKey": "k1", "fromWalletId": "w1", "toWalletId": "w2", "amount": 999, // different body
    })
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusConflict {
        t.Fatalf("status: got %d want 409", resp.StatusCode)
    }
}
```

What to cover for any new endpoint:
- Happy path (200, expected body).
- Each validation error (400, 404, 409 as appropriate).
- Idempotency replay returns the same id.
- Idempotency body conflict returns 409.

---

## Pattern C — concurrency test (mandatory for any code that touches wallet balances)

```go
func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
    testdb.Reset(t)
    tsvc, wsvc := newTransferSvc(t)
    ctx := context.Background()

    const (
        startingBalance int64 = 1000
        perTransfer     int64 = 100
        numTransfers          = 20 // 20 * 100 = 2000 attempted, only 10 should succeed
    )
    seedWallets(t, wsvc, map[string]int64{"src": startingBalance, "dst": 0})

    var wg sync.WaitGroup
    var processed, failed int64
    wg.Add(numTransfers)
    for i := 0; i < numTransfers; i++ {
        go func(i int) {
            defer wg.Done()
            tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
                IdempotencyKey: keyFor(i),
                FromWalletID:   "src",
                ToWalletID:     "dst",
                Amount:         perTransfer,
            })
            if err != nil {
                t.Errorf("transfer %d: %v", i, err)
                return
            }
            switch tr.State {
            case domain.StateProcessed:
                atomic.AddInt64(&processed, 1)
            case domain.StateFailed:
                atomic.AddInt64(&failed, 1)
            }
        }(i)
    }
    wg.Wait()

    src, _ := wsvc.Get(ctx, "src")
    dst, _ := wsvc.Get(ctx, "dst")

    if src.Balance < 0 {
        t.Fatalf("source went negative: %d", src.Balance)
    }
    if src.Balance+dst.Balance != startingBalance {
        t.Fatalf("ledger not conserved: src=%d dst=%d", src.Balance, dst.Balance)
    }
    if processed != startingBalance/perTransfer {
        t.Fatalf("processed: got %d want %d", processed, startingBalance/perTransfer)
    }
}
```

Invariants every concurrency test must assert:
- [ ] Source balance never goes negative.
- [ ] Sum of all wallet balances is conserved (debits == credits).
- [ ] No transfer ends up in an unexpected state (only PROCESSED or
      FAILED).
- [ ] Idempotency keys are distinct UNLESS the test is specifically
      checking same-key collapsing (Pattern D).

---

## Pattern D — concurrent same-key collapsing

When N goroutines fire with the same idempotency key, exactly one
transfer must be created.

```go
var wg sync.WaitGroup
ids := make(chan string, goroutines)
wg.Add(goroutines)
for i := 0; i < goroutines; i++ {
    go func() {
        defer wg.Done()
        tr, err := tsvc.Create(ctx, req)
        if err != nil {
            t.Errorf("create: %v", err)
            return
        }
        ids <- tr.ID.String()
    }()
}
wg.Wait()
close(ids)

var seen string
for id := range ids {
    if seen == "" {
        seen = id
    } else if id != seen {
        t.Fatalf("different transfer ids observed: %s vs %s", seen, id)
    }
}
```

Asserts: every caller observes the same transfer id; the wallet is
debited exactly once regardless of how many duplicate requests arrived.

---

## Hygiene

- Always run with `-race` and `-p 1`:
  ```sh
  DATABASE_URL=postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable \
    go test -race -tags=integration -count=1 -timeout=300s -p 1 ./...
  ```
- `-count=1` is mandatory — Go caches test results by default and we do
  not want a stale cache hiding a regression.
- `-p 1` is mandatory — every package shares the one Postgres, and
  parallel package binaries would let one `TRUNCATE` mid-test in another.
  `make test-int` already sets this.
- `-timeout=300s` because CI is sometimes slow to spin up the postgres
  service container; local runs are fast.
- Update the **Test coverage matrix** in [README.md](../../../README.md)
  whenever a new test is added that maps to an assignment requirement.

---

## Pattern E — GET endpoint (single resource by id)

```go
func TestHTTP_GetTransferByID_HappyPath(t *testing.T) {
    testdb.Reset(t)
    srv := newServer(t)
    // Arrange: create the resource so we have an id to fetch.
    mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w1", "balance": 100}))
    mustOK(t, postJSON(t, srv.URL+"/wallets", map[string]any{"id": "w2", "balance": 0}))
    r := postJSON(t, srv.URL+"/transfers", map[string]any{
        "idempotencyKey": "k", "fromWalletId": "w1", "toWalletId": "w2", "amount": 10,
    })
    mustOK(t, r)
    var created map[string]any
    _ = json.NewDecoder(r.Body).Decode(&created)
    r.Body.Close()

    // Act
    resp, err := http.Get(srv.URL + "/transfers/" + created["id"].(string))
    if err != nil { t.Fatal(err) }
    defer resp.Body.Close()

    // Assert: round-trip yields the same id.
    if resp.StatusCode != http.StatusOK { t.Fatalf("status: %d", resp.StatusCode) }
    var got map[string]any
    _ = json.NewDecoder(resp.Body).Decode(&got)
    if got["id"] != created["id"] { t.Fatalf("id mismatch") }
}
```

For every GET-by-id endpoint, also add:

- **`…_NotFound`** — return 404 for a well-formed-but-absent id.
- **`…_MalformedID`** — return 400 for an unparseable id (e.g. not
  a UUID). The handler must reject without ever calling the
  service.

See `handler.TestHTTP_GetTransferByID_*` for the full set.

## Pattern F — paginated list endpoint (cursor walk)

```go
func TestHTTP_ListWalletTransfers_HappyPathWithPagination(t *testing.T) {
    testdb.Reset(t)
    srv := newServer(t)
    // Arrange: seed >1 page of resources.
    const N = 5
    // ... create N transfers ...

    // Act: walk pages until the server stops returning a cursor.
    seen := map[string]bool{}
    cursor := ""
    for page := 0; page < 10; page++ {
        url := srv.URL + "/wallets/src/transfers?limit=2"
        if cursor != "" { url += "&before=" + cursor }
        resp, _ := http.Get(url)
        var body struct {
            Transfers  []map[string]any `json:"transfers"`
            NextCursor string           `json:"nextCursor"`
        }
        _ = json.NewDecoder(resp.Body).Decode(&body)
        resp.Body.Close()

        for _, tr := range body.Transfers {
            id := tr["id"].(string)
            if seen[id] { t.Fatalf("duplicate id across pages: %s", id) }
            seen[id] = true
        }
        if body.NextCursor == "" { break } // documented end-of-history signal
        cursor = body.NextCursor
    }
    if len(seen) != N { t.Fatalf("union mismatch") }
}
```

For every paginated endpoint, also cover:

- **`…_EmptyForUnknownWallet`** — 200 with `transfers: []`, NOT 404.
  Returning 404 on unknown wallets would let an attacker enumerate
  valid wallet ids.
- **`…_BadLimit`** — 400 for non-numeric / negative `?limit`.
- **`…_BadBefore`** — 400 for malformed `?before` timestamps.
- **`…_LimitOneReturnsCursor`** — when results > page size, the
  response MUST include `nextCursor`.
- **`…_NoCursorOnLastPage`** — short page → `nextCursor` is
  omitted (the documented end-of-history signal).

See `handler.TestHTTP_ListWalletTransfers_*` for the full set.

## Pattern G — metrics test (no DB needed; runs as unit test)

```go
// internal/metrics/metrics_test.go
func TestTransfersTotal_IncrementsByLabel(t *testing.T) {
    before := testutil.ToFloat64(TransfersTotal.WithLabelValues("PROCESSED"))
    TransfersTotal.WithLabelValues("PROCESSED").Inc()
    after := testutil.ToFloat64(TransfersTotal.WithLabelValues("PROCESSED"))
    if after != before+1 { t.Fatalf("not incremented: %v -> %v", before, after) }
}
```

For every new collector, also add:

- **Registration test** — assert it's present in `Registry`.
- **Increment / observe test** — verify the metric actually
  responds to the action.
- **Exposition test** — `Handler()` returns Prometheus text
  format including the new series.

For the HTTP metrics middleware specifically, also add a
cardinality safety test: hit 3+ distinct paths matching the same
route pattern and assert exactly one series increments (not 3).
See `metrics.TestHTTPMiddleware_LabelsByRoute`.

## What not to do

| Anti-pattern | Why it's banned |
|---|---|
| Using a Go mock for `*sql.DB` | The actual contract under test IS the DB |
| Skipping `testdb.Reset(t)` | Leftover rows leak between tests |
| Subtests sharing seeded state | Order-dependence; flaky under `-shuffle on` |
| Hard-coded `time.Sleep` to "wait for the goroutine" | Race-prone; use `sync.WaitGroup` |
| Asserting on private method invocation | Coupling tests to implementation |
| Forgetting `//go:build integration` | Test runs under `make test` and fails because Docker isn't ready |
| Generic `assert.Equal` libs | Stdlib `t.Fatalf` is enough and gives clearer failure messages |
