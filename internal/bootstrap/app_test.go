package bootstrap_test

import (
	"testing"

	"wallet-transfer-assignment/internal/bootstrap"
)

// Dependencies are created lazily, so wiring can be checked without a database.
func TestNewAppWiresTheTransferHandler(t *testing.T) {
	app := bootstrap.NewApp(nil)

	if app == nil || app.TransferHandler == nil {
		t.Fatal("NewApp must return an App with a TransferHandler")
	}
}
