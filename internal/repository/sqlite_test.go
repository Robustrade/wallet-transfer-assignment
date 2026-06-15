package repository

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/ports"
)

func TestTransactionHelpersApplyConnectionPragmas(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)

	tests := []struct {
		name string
		run  func(context.Context, func(context.Context, ports.TransferRepository) error) error
	}{
		{name: "write transaction", run: store.WithTx},
		{name: "read transaction", run: store.WithReadTx},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
				tx, ok := repo.(*SQLiteTx)
				if !ok {
					t.Fatalf("repo type = %T, want *SQLiteTx", repo)
				}

				var foreignKeys int
				if err := tx.conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
					t.Fatalf("query foreign_keys pragma: %v", err)
				}
				if foreignKeys != 1 {
					t.Fatalf("foreign_keys pragma = %d, want 1", foreignKeys)
				}

				var busyTimeout int
				if err := tx.conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
					t.Fatalf("query busy_timeout pragma: %v", err)
				}
				if busyTimeout != sqliteBusyTimeoutMillis {
					t.Fatalf("busy_timeout pragma = %d, want %d", busyTimeout, sqliteBusyTimeoutMillis)
				}

				return nil
			})
			if err != nil {
				t.Fatalf("transaction returned error: %v", err)
			}
		})
	}
}

func TestCreateWalletMapsOnlyUniquenessConstraintToAlreadyExists(t *testing.T) {
	ctx := context.Background()
	store := newTestSQLiteStore(t)
	now := time.Now().UTC()

	wallet := domain.Wallet{
		ID:        "wallet_1",
		Balance:   100,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err := store.WithTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		return repo.CreateWallet(ctx, wallet)
	})
	if err != nil {
		t.Fatalf("CreateWallet returned error: %v", err)
	}

	err = store.WithTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		return repo.CreateWallet(ctx, wallet)
	})
	assertAppErrorCode(t, err, domain.ErrorWalletAlreadyExists)

	invalidWallet := domain.Wallet{
		ID:        "wallet_invalid",
		Balance:   -1,
		CreatedAt: now,
		UpdatedAt: now,
	}
	err = store.WithTx(ctx, func(ctx context.Context, repo ports.TransferRepository) error {
		return repo.CreateWallet(ctx, invalidWallet)
	})
	if err == nil {
		t.Fatal("CreateWallet with invalid balance returned nil, want error")
	}

	var appErr *domain.AppError
	if errors.As(err, &appErr) && appErr.Code == domain.ErrorWalletAlreadyExists {
		t.Fatalf("check constraint mapped to %s, want wrapped persistence error", domain.ErrorWalletAlreadyExists)
	}
}

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "wallet.db")
	store, err := OpenSQLite(ctx, fmt.Sprintf("file:%s", dbPath))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	})

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	return store
}

func assertAppErrorCode(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()

	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want AppError with code %s", err, want)
	}
	if appErr.Code != want {
		t.Fatalf("error code = %s, want %s", appErr.Code, want)
	}
}
