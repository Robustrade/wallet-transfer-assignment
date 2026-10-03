package apitest

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestReplayReturnsTheOriginalResponse(t *testing.T) {
	t.Parallel()

	t.Run("PROCESSED transfer", func(t *testing.T) {
		beginTest(t)
		from := createWallet(t, 1000)
		to := createWallet(t, 0)
		body := transferBody(newKey(), from, to, 300)

		original := postTransfer(t, body)
		original.transfer(t, http.StatusCreated)
		replay := postTransfer(t, body)

		assertSameResponse(t, replay, original)
		assertMovedOnce(t, from, 700, to, 300)
	})

	t.Run("same values with other field order and spacing", func(t *testing.T) {
		beginTest(t)
		from := createWallet(t, 1000)
		to := createWallet(t, 0)
		key := newKey()

		original := postTransfer(t, transferBody(key, from, to, 300))
		original.transfer(t, http.StatusCreated)
		replay := postTransfer(t, fmt.Sprintf(
			`{ "amount": 300, "toWalletId": %q, "fromWalletId": %q, "idempotencyKey": %q }`, to, from, key))

		assertSameResponse(t, replay, original)
		assertMovedOnce(t, from, 700, to, 300)
	})

	t.Run("FAILED transfer, even after the balance has grown", func(t *testing.T) {
		beginTest(t)
		funder := createWallet(t, 1000)
		from := createWallet(t, 100)
		to := createWallet(t, 0)
		body := transferBody(newKey(), from, to, 150)

		original := postTransfer(t, body)
		original.transfer(t, http.StatusUnprocessableEntity)
		postTransfer(t, transferBody(newKey(), funder, from, 100)).transfer(t, http.StatusCreated)
		replay := postTransfer(t, body)

		assertSameResponse(t, replay, original)
		if balance := balanceOf(t, from); balance != 200 {
			t.Errorf("source balance = %d, want 200", balance)
		}
		if n := transferCount(t, to); n != 1 {
			t.Errorf("%d transfers stored for the destination, want only the original", n)
		}
		if balance := balanceOf(t, to); balance != 0 {
			t.Errorf("destination balance = %d, want 0", balance)
		}
	})
}

func TestReplayAfterRestart(t *testing.T) {
	beginTest(t)
	from := createWallet(t, 1000)
	to := createWallet(t, 0)
	body := transferBody(newKey(), from, to, 300)

	original := postTransfer(t, body)
	original.transfer(t, http.StatusCreated)
	// A new server and pool on the same database, as after a restart.
	replay := postTransferTo(t, startServer(t, databaseURL, discardLogger), body)

	assertSameResponse(t, replay, original)
	assertMovedOnce(t, from, 700, to, 300)
}

func TestKeyReusedWithADifferentBody(t *testing.T) {
	t.Parallel()

	// Each case reuses the key of a 100 transfer from -> to; other is a third
	// wallet.
	tests := []struct {
		name string
		body func(key, from, to, other string) string
	}{
		{"different amount", func(key, from, to, _ string) string {
			return transferBody(key, from, to, 200)
		}},
		{"different source", func(key, _, to, other string) string {
			return transferBody(key, other, to, 100)
		}},
		{"different destination", func(key, from, _, other string) string {
			return transferBody(key, from, other, 100)
		}},
		// Spec §6: the key check (step 6) comes before the wallet check (step 7).
		{"unknown destination", func(key, from, _, _ string) string {
			return transferBody(key, from, "wallet-unknown-"+uuid.NewString(), 100)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			from := createWallet(t, 1000)
			to := createWallet(t, 1000)
			other := createWallet(t, 1000)
			key := newKey()
			original := postTransfer(t, transferBody(key, from, to, 100)).transfer(t, http.StatusCreated)

			got := postTransfer(t, tt.body(key, from, to, other)).errorBody(t, http.StatusConflict)

			if got.Error.Code != "IDEMPOTENCY_KEY_REUSED" {
				t.Errorf("code = %s, want IDEMPOTENCY_KEY_REUSED", got.Error.Code)
			}
			wantStored := storedTransfer{key: key, from: from, to: to, amount: 100, state: "PROCESSED"}
			if stored := loadTransfer(t, original.TransferID); stored != wantStored {
				t.Errorf("original transfer = %+v, want %+v", stored, wantStored)
			}
			assertMovedOnce(t, from, 900, to, 1100)
			assertUntouched(t, other, 1000)
		})
	}
}

func TestRejectedRequestDoesNotUseUpItsKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     func(key, from, to string) string
		wantCode string
	}{
		{"rejected before the transaction", func(key, from, to string) string {
			return transferBody(key, from, to, 0)
		}, "INVALID_AMOUNT"},
		{"rejected inside the transaction", func(key, from, _ string) string {
			return transferBody(key, from, "wallet-unknown-"+uuid.NewString(), 100)
		}, "WALLET_NOT_FOUND"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			from := createWallet(t, 1000)
			to := createWallet(t, 0)
			key := newKey()

			rejected := postTransfer(t, tt.body(key, from, to)).errorBody(t, http.StatusBadRequest)
			if rejected.Error.Code != tt.wantCode {
				t.Fatalf("code = %s, want %s", rejected.Error.Code, tt.wantCode)
			}
			got := postTransfer(t, transferBody(key, from, to, 100)).transfer(t, http.StatusCreated)

			if got.State != "PROCESSED" {
				t.Errorf("state = %q, want PROCESSED", got.State)
			}
			assertMovedOnce(t, from, 900, to, 100)
		})
	}
}

func TestConcurrentRequestsWithOneKey(t *testing.T) {
	beginTest(t)
	from := createWallet(t, 1000)
	to := createWallet(t, 0)
	body := transferBody(newKey(), from, to, 100)

	bodies := make([]string, 20)
	for i := range bodies {
		bodies[i] = body
	}
	responses := postConcurrently(t, bodies)

	responses[0].transfer(t, http.StatusCreated)
	for i, resp := range responses[1:] {
		if resp.status != responses[0].status || string(resp.body) != string(responses[0].body) {
			t.Errorf("response %d = %d %s, want %d %s", i+1, resp.status, resp.body, responses[0].status, responses[0].body)
		}
	}
	assertMovedOnce(t, from, 900, to, 100)
}

// assertMovedOnce fails t unless exactly one transfer involves from and the
// two balances are as given.
func assertMovedOnce(t *testing.T, from string, wantFrom int64, to string, wantTo int64) {
	t.Helper()
	if n := transferCount(t, from); n != 1 {
		t.Errorf("%d transfers stored for the source, want 1", n)
	}
	if balance := balanceOf(t, from); balance != wantFrom {
		t.Errorf("source balance = %d, want %d", balance, wantFrom)
	}
	if balance := balanceOf(t, to); balance != wantTo {
		t.Errorf("destination balance = %d, want %d", balance, wantTo)
	}
}
