// Package integration_test holds the tests that need a real PostgreSQL:
// schema constraints, repository behaviour, the transfer service (idempotency,
// atomicity, concurrency) and the HTTP API end to end.
package integration_test

import (
	"os"
	"testing"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Run(m)) }
