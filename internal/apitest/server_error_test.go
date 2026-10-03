package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

func TestDatabaseUnavailable(t *testing.T) {
	beginTest(t)
	from := createWallet(t, 1000)
	to := createWallet(t, 0)
	port, err := freePort()
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	// Nothing listens on port, so the server cannot reach its database.
	server := startServer(t, fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres", port), discardLogger)

	got := postTransferTo(t, server, transferBody(newKey(), from, to, 100)).errorBody(t, http.StatusInternalServerError)

	if got.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("code = %s, want INTERNAL_ERROR", got.Error.Code)
	}
	assertUntouched(t, from, 1000)
	assertUntouched(t, to, 0)
}
