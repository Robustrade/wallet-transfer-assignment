//go:build integration

package service_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

// seedTransfers wires up a wallet and creates N successful
// transfers FROM it to a sink wallet, returning the transfers in
// the order they were created (oldest first).
func seedTransfers(t *testing.T, n int) (*service.TransferService, *service.WalletService, []domain.Transfer) {
	t.Helper()
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	if _, err := wsvc.Create(ctx, "src", int64(n*100)); err != nil {
		t.Fatal(err)
	}
	if _, err := wsvc.Create(ctx, "sink", 0); err != nil {
		t.Fatal(err)
	}

	out := make([]domain.Transfer, 0, n)
	for i := 0; i < n; i++ {
		// Sleep a millisecond so created_at strictly increases —
		// the cursor pagination orders by created_at DESC and the
		// test wants deterministic order.
		time.Sleep(time.Millisecond)
		tr, err := tsvc.Create(ctx, service.CreateTransferRequest{
			IdempotencyKey: "hist-" + strconv.Itoa(i),
			FromWalletID:   "src",
			ToWalletID:     "sink",
			Amount:         1,
		})
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, *tr)
	}
	return tsvc, wsvc, out
}

// TestListTransfersByWallet_EmptyHistory covers the no-transfers
// case: the service returns nil slice + nil cursor without error.
// A wallet with no history is a normal state, not a failure.
func TestListTransfersByWallet_EmptyHistory(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	if _, err := wsvc.Create(ctx, "empty", 0); err != nil {
		t.Fatal(err)
	}
	rows, next, err := tsvc.ListTransfersByWallet(ctx, "empty", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(rows))
	}
	if next != nil {
		t.Fatalf("expected nil cursor, got %v", next)
	}
}

// TestListTransfersByWallet_EmptyWalletID rejects the empty-id
// request rather than silently returning the global transfer
// list.
func TestListTransfersByWallet_EmptyWalletID(t *testing.T) {
	testdb.Reset(t)
	tsvc, _ := newTransferSvc(t)
	if _, _, err := tsvc.ListTransfersByWallet(context.Background(), "", 0, nil); err == nil {
		t.Fatal("expected error on empty wallet id")
	}
}

// TestListTransfersByWallet_OrderedByCreatedAtDesc verifies the
// most-recent-first ordering.
func TestListTransfersByWallet_OrderedByCreatedAtDesc(t *testing.T) {
	testdb.Reset(t)
	tsvc, _, created := seedTransfers(t, 3)
	rows, _, err := tsvc.ListTransfersByWallet(context.Background(), "src", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows want 3", len(rows))
	}
	// `created` is oldest-first; `rows` should be newest-first.
	if rows[0].ID != created[2].ID {
		t.Fatalf("first row should be newest: got %s want %s", rows[0].ID, created[2].ID)
	}
	if rows[2].ID != created[0].ID {
		t.Fatalf("last row should be oldest: got %s want %s", rows[2].ID, created[0].ID)
	}
}

// TestListTransfersByWallet_LimitDefault verifies that a 0 limit
// triggers the documented default (DefaultTransferListLimit).
func TestListTransfersByWallet_LimitDefault(t *testing.T) {
	testdb.Reset(t)
	tsvc, _, _ := seedTransfers(t, 3)
	rows, _, err := tsvc.ListTransfersByWallet(context.Background(), "src", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected all 3 with default limit, got %d", len(rows))
	}
}

// TestListTransfersByWallet_LimitClamp checks the upper clamp:
// requesting limit > MaxTransferListLimit must NOT panic and must
// NOT return more than the max.
func TestListTransfersByWallet_LimitClamp(t *testing.T) {
	testdb.Reset(t)
	tsvc, _, _ := seedTransfers(t, 3)
	rows, _, err := tsvc.ListTransfersByWallet(context.Background(), "src", 1<<30, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) > service.MaxTransferListLimit {
		t.Fatalf("limit not clamped: got %d > max %d", len(rows), service.MaxTransferListLimit)
	}
}

// TestListTransfersByWallet_PaginationByCursor walks the cursor
// across all pages and verifies the union equals the seeded set
// without duplicates.
func TestListTransfersByWallet_PaginationByCursor(t *testing.T) {
	testdb.Reset(t)
	tsvc, _, created := seedTransfers(t, 5)

	seen := map[string]bool{}
	var cursor *time.Time
	for page := 0; page < 10; page++ {
		rows, next, err := tsvc.ListTransfersByWallet(context.Background(), "src", 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			if seen[r.ID.String()] {
				t.Fatalf("duplicate id across pages: %s", r.ID)
			}
			seen[r.ID.String()] = true
		}
		if next == nil {
			break
		}
		cursor = next
	}
	if len(seen) != len(created) {
		t.Fatalf("paginated set: got %d want %d", len(seen), len(created))
	}
}

// TestListTransfersByWallet_BothFromAndTo covers a wallet that
// participates in transfers in both directions: the result must
// include both source-side and destination-side history.
func TestListTransfersByWallet_BothFromAndTo(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"a": 100, "b": 100})

	for i := 0; i < 3; i++ {
		time.Sleep(time.Millisecond)
		// alternating directions
		from, to := "a", "b"
		if i%2 == 1 {
			from, to = "b", "a"
		}
		_, err := tsvc.Create(ctx, service.CreateTransferRequest{
			IdempotencyKey: "biway-" + strconv.Itoa(i),
			FromWalletID:   from, ToWalletID: to, Amount: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, _, err := tsvc.ListTransfersByWallet(ctx, "a", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows for wallet 'a' (both from + to), got %d", len(rows))
	}
}

// TestListTransfersByWallet_CursorBeforeAll returns the empty set
// when the caller pages past the start of history.
func TestListTransfersByWallet_CursorBeforeAll(t *testing.T) {
	testdb.Reset(t)
	tsvc, _, _ := seedTransfers(t, 3)
	long_ago := time.Unix(0, 0)
	rows, next, err := tsvc.ListTransfersByWallet(context.Background(), "src", 10, &long_ago)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 || next != nil {
		t.Fatalf("expected empty set, got %d rows, next=%v", len(rows), next)
	}
}

// TestListTransfersByWallet_FailedTransfersIncluded confirms that
// FAILED transfers are part of the history (they're committed
// rows with a state value, not deleted).
func TestListTransfersByWallet_FailedTransfersIncluded(t *testing.T) {
	testdb.Reset(t)
	tsvc, wsvc := newTransferSvc(t)
	ctx := context.Background()
	seedWallets(t, wsvc, map[string]int64{"poor": 1, "rich": 1000})
	// One success, one fail.
	if _, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "ok", FromWalletID: "rich", ToWalletID: "poor", Amount: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tsvc.Create(ctx, service.CreateTransferRequest{
		IdempotencyKey: "fail", FromWalletID: "poor", ToWalletID: "rich", Amount: 999999,
	}); err != nil {
		t.Fatal(err)
	}

	rows, _, err := tsvc.ListTransfersByWallet(ctx, "poor", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (success + failed), got %d", len(rows))
	}
	var sawFailed, sawProcessed bool
	for _, r := range rows {
		switch r.State {
		case domain.StateFailed:
			sawFailed = true
		case domain.StateProcessed:
			sawProcessed = true
		}
	}
	if !sawFailed || !sawProcessed {
		t.Fatalf("expected both states present: failed=%v processed=%v", sawFailed, sawProcessed)
	}
}
