package apitest

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRejectedRequests(t *testing.T) {
	t.Parallel()

	// In each body, $KEY, $FROM, and $TO become a fresh key and two fresh
	// wallets, and $LONG becomes a 256-character string.
	tests := []struct {
		name     string
		body     string
		wantCode string
	}{
		// Spec §6 step 1: the body is a single JSON object.
		{"invalid JSON", `{"idempotencyKey":`, "INVALID_JSON"},
		{"empty body", ``, "INVALID_JSON"},
		{"array", `[]`, "INVALID_JSON"},
		{"string", `"transfer"`, "INVALID_JSON"},
		{"null", `null`, "INVALID_JSON"},
		{"trailing object", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":100} {}`, "INVALID_JSON"},
		{"trailing bracket", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":100}]`, "INVALID_JSON"},

		// Step 2: no fields beyond the four in spec §2.
		{"extra field", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":100,"currency":"USD"}`, "UNKNOWN_FIELD"},
		{"field name in another case", `{"IdempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":100}`, "UNKNOWN_FIELD"},

		// Step 3: every field is present and not null; strings are JSON
		// strings of 1–255 characters.
		{"missing idempotencyKey", `{"fromWalletId":"$FROM","toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"null fromWalletId", `{"idempotencyKey":"$KEY","fromWalletId":null,"toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"empty toWalletId", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"","amount":100}`, "INVALID_FIELD"},
		{"idempotencyKey too long", `{"idempotencyKey":"$LONG","fromWalletId":"$FROM","toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"idempotencyKey is a number", `{"idempotencyKey":123,"fromWalletId":"$FROM","toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"fromWalletId is an object", `{"idempotencyKey":"$KEY","fromWalletId":{},"toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"NUL in idempotencyKey", `{"idempotencyKey":"$KEY\u0000","fromWalletId":"$FROM","toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"NUL in fromWalletId", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM\u0000","toWalletId":"$TO","amount":100}`, "INVALID_FIELD"},
		{"NUL in toWalletId", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO\u0000","amount":100}`, "INVALID_FIELD"},
		{"missing amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO"}`, "INVALID_FIELD"},
		{"null amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":null}`, "INVALID_FIELD"},

		// Step 4: amount is a JSON integer from 1 to int64 max.
		{"amount as a string", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":"100"}`, "INVALID_AMOUNT"},
		{"fractional amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":100.5}`, "INVALID_AMOUNT"},
		{"amount with a decimal point", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":100.0}`, "INVALID_AMOUNT"},
		{"amount in exponent form", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":1e2}`, "INVALID_AMOUNT"},
		{"zero amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":0}`, "INVALID_AMOUNT"},
		{"negative amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":-5}`, "INVALID_AMOUNT"},
		{"amount above int64 max", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":9223372036854775808}`, "INVALID_AMOUNT"},
		{"boolean amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$TO","amount":true}`, "INVALID_AMOUNT"},

		// Step 5: the wallets differ.
		{"same wallet", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$FROM","amount":100}`, "SAME_WALLET"},

		// The earliest failing step decides the error.
		{"unknown field before missing field", `{"fromWalletId":"$FROM","toWalletId":"$TO","amount":100,"note":"x"}`, "UNKNOWN_FIELD"},
		{"wrong type before invalid amount", `{"idempotencyKey":123,"fromWalletId":"$FROM","toWalletId":"$TO","amount":0}`, "INVALID_FIELD"},
		{"missing field before invalid amount", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","amount":1e2}`, "INVALID_FIELD"},
		{"null amount before same wallet", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$FROM","amount":null}`, "INVALID_FIELD"},
		{"invalid amount before same wallet", `{"idempotencyKey":"$KEY","fromWalletId":"$FROM","toWalletId":"$FROM","amount":0}`, "INVALID_AMOUNT"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			from := createWallet(t, 1000)
			to := createWallet(t, 1000)
			body := strings.NewReplacer(
				"$KEY", newKey(),
				"$FROM", from,
				"$TO", to,
				"$LONG", strings.Repeat("k", 256),
			).Replace(tt.body)

			got := postTransfer(t, body).errorBody(t, http.StatusBadRequest)

			if got.Error.Code != tt.wantCode {
				t.Errorf("code = %s, want %s", got.Error.Code, tt.wantCode)
			}
			for _, wallet := range []string{from, to} {
				assertUntouched(t, wallet, 1000)
			}
		})
	}
}

func TestUnknownWallet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		unknownFrom, unknownTo bool
	}{
		{"unknown source", true, false},
		{"unknown destination", false, true},
		{"both unknown", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			wallets := []string{createWallet(t, 1000), createWallet(t, 1000)}
			from, to := wallets[0], wallets[1]
			if tt.unknownFrom {
				from = "wallet-unknown-" + uuid.NewString()
			}
			if tt.unknownTo {
				to = "wallet-unknown-" + uuid.NewString()
			}

			got := postTransfer(t, transferBody(newKey(), from, to, 100)).errorBody(t, http.StatusBadRequest)

			if got.Error.Code != "WALLET_NOT_FOUND" {
				t.Errorf("code = %s, want WALLET_NOT_FOUND", got.Error.Code)
			}
			for _, wallet := range wallets {
				assertUntouched(t, wallet, 1000)
			}
		})
	}
}

func TestRequestBodyLimit(t *testing.T) {
	t.Parallel()

	const limit = 64 << 10 // spec §2

	tests := []struct {
		name       string
		body       func(valid string) string
		wantStatus int
	}{
		{"exactly 64 KiB", func(valid string) string {
			return padInside(valid, limit)
		}, http.StatusCreated},
		{"one byte over, padded inside the object", func(valid string) string {
			return padInside(valid, limit+1)
		}, http.StatusRequestEntityTooLarge},
		{"one byte over, padded after the object", func(valid string) string {
			return valid + strings.Repeat(" ", limit+1-len(valid))
		}, http.StatusRequestEntityTooLarge},
		// Spec §6 step 1: the size check comes before the JSON check.
		{"over the limit and not JSON", func(string) string {
			return strings.Repeat("x", limit+1)
		}, http.StatusRequestEntityTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			from := createWallet(t, 1000)
			to := createWallet(t, 1000)

			resp := postTransfer(t, tt.body(transferBody(newKey(), from, to, 100)))

			if tt.wantStatus == http.StatusCreated {
				resp.transfer(t, http.StatusCreated)
				return
			}
			got := resp.errorBody(t, tt.wantStatus)
			if got.Error.Code != "REQUEST_TOO_LARGE" {
				t.Errorf("code = %s, want REQUEST_TOO_LARGE", got.Error.Code)
			}
			assertUntouched(t, from, 1000)
			assertUntouched(t, to, 1000)
		})
	}
}

// padInside pads a JSON object with spaces before its closing brace, so the
// body is exactly size bytes and still valid.
func padInside(object string, size int) string {
	return object[:len(object)-1] + strings.Repeat(" ", size-len(object)) + "}"
}
