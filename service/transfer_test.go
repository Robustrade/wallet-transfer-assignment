package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/Robustrade/wallet-transfer-assignment/service"
	"github.com/google/uuid"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func setupTestService(t *testing.T) (*service.TransferService, *sql.DB) {
	t.Helper()

	db, err := sql.Open(
		"pgx",
		"postgres://pratiskha@localhost:5432/wallet_transfer?sslmode=disable",
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}

	walletRepo := repository.NewWalletRepository(db)
	transferRepo := repository.NewTransferRepository(db)
	ledgerRepo := repository.NewLedgerRepository(db)

	svc := service.NewTransferService(
		db,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	t.Cleanup(func() {
		_ = db.Close()
	})

	return svc, db
}

func createWallet(t *testing.T, db *sql.DB, balance int64) string {
	t.Helper()

	id := "test-wallet-" + uuid.New().String()

	_, err := db.Exec(`
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2)
	`, id, balance)

	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM wallets WHERE id = $1`, id)
	})

	return id
}

func TestCreateTransfer_Success(t *testing.T) {
	svc, db := setupTestService(t)

	fromWallet := createWallet(t, db, 10000)
	toWallet := createWallet(t, db, 5000)

	transfer, err := svc.CreateTransfer(
		context.Background(),
		model.TransferRequest{
			IdempotencyKey: "test-success-" + uuid.New().String(),
			FromWalletID:   fromWallet,
			ToWalletID:     toWallet,
			Amount:         "25.50",
		},
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if transfer.Status != model.StatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", transfer.Status)
	}

	var fromBalance, toBalance int64

	err = db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		fromWallet,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		toWallet,
	).Scan(&toBalance)
	if err != nil {
		t.Fatal(err)
	}

	if fromBalance != 7450 {
		t.Fatalf("expected source balance 7450, got %d", fromBalance)
	}

	if toBalance != 7550 {
		t.Fatalf("expected destination balance 7550, got %d", toBalance)
	}
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	svc, db := setupTestService(t)

	fromWallet := createWallet(t, db, 10000)
	toWallet := createWallet(t, db, 5000)

	key := "test-idempotency-" + uuid.New().String()

	req := model.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   fromWallet,
		ToWalletID:     toWallet,
		Amount:         "100",
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	second, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second transfer failed: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf(
			"expected same transfer ID, got %s and %s",
			first.ID,
			second.ID,
		)
	}

	var fromBalance int64

	err = db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		fromWallet,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatal(err)
	}

	if fromBalance != 0 {
		t.Fatalf("expected source balance 0, got %d", fromBalance)
	}
}

func TestCreateTransfer_IdempotencyConflict(t *testing.T) {
	svc, db := setupTestService(t)

	fromWallet := createWallet(t, db, 10000)
	toWallet := createWallet(t, db, 5000)

	key := "test-conflict-" + uuid.New().String()

	firstReq := model.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   fromWallet,
		ToWalletID:     toWallet,
		Amount:         "100",
	}

	_, err := svc.CreateTransfer(context.Background(), firstReq)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	conflictingReq := firstReq
	conflictingReq.Amount = "200"

	_, err = svc.CreateTransfer(
		context.Background(),
		conflictingReq,
	)

	if err != service.ErrIdempotencyConflict {
		t.Fatalf(
			"expected idempotency conflict, got %v",
			err,
		)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, db := setupTestService(t)

	fromWallet := createWallet(t, db, 100)
	toWallet := createWallet(t, db, 5000)

	transfer, err := svc.CreateTransfer(
		context.Background(),
		model.TransferRequest{
			IdempotencyKey: "test-insufficient-" + uuid.New().String(),
			FromWalletID:   fromWallet,
			ToWalletID:     toWallet,
			Amount:         "200",
		},
	)

	if err != service.ErrInsufficientFunds {
		t.Fatalf(
			"expected insufficient funds error, got %v",
			err,
		)
	}

	if transfer != nil {
		t.Fatalf("expected nil transfer, got %+v", transfer)
	}

	var fromBalance, toBalance int64

	err = db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		fromWallet,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		toWallet,
	).Scan(&toBalance)
	if err != nil {
		t.Fatal(err)
	}

	if fromBalance != 100 {
		t.Fatalf("expected source balance 100, got %d", fromBalance)
	}

	if toBalance != 5000 {
		t.Fatalf("expected destination balance 5000, got %d", toBalance)
	}
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	svc, _ := setupTestService(t)

	_, err := svc.CreateTransfer(
		context.Background(),
		model.TransferRequest{
			IdempotencyKey: "test-wallet-not-found-" + uuid.New().String(),
			FromWalletID:   "does-not-exist",
			ToWalletID:     "also-does-not-exist",
			Amount:         "100",
		},
	)

	if err == nil {
		t.Fatal("expected wallet not found error")
	}

	if !strings.Contains(err.Error(), "wallet not found") {
		t.Fatalf("expected wallet not found error, got %v", err)
	}
}

func TestCreateTransfer_LedgerEntries(t *testing.T) {
	svc, db := setupTestService(t)

	fromWallet := createWallet(t, db, 10000)
	toWallet := createWallet(t, db, 5000)

	transfer, err := svc.CreateTransfer(
		context.Background(),
		model.TransferRequest{
			IdempotencyKey: "test-ledger-" + uuid.New().String(),
			FromWalletID:   fromWallet,
			ToWalletID:     toWallet,
			Amount:         "100",
		},
	)

	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	var count int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
	`, transfer.ID).Scan(&count)

	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", count)
	}

	var debitCount, creditCount int

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
		  AND entry_type = 'DEBIT'
	`, transfer.ID).Scan(&debitCount)

	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = $1
		  AND entry_type = 'CREDIT'
	`, transfer.ID).Scan(&creditCount)

	if err != nil {
		t.Fatal(err)
	}

	if debitCount != 1 {
		t.Fatalf("expected 1 debit entry, got %d", debitCount)
	}

	if creditCount != 1 {
		t.Fatalf("expected 1 credit entry, got %d", creditCount)
	}
}

func TestCreateTransfer_ConcurrentTransfers(t *testing.T) {
	svc, db := setupTestService(t)

	fromWallet := createWallet(t, db, 10000)
	toWallet := createWallet(t, db, 0)

	const transferCount = 10

	errCh := make(chan error, transferCount)

	for i := 0; i < transferCount; i++ {
		go func(i int) {
			_, err := svc.CreateTransfer(
				context.Background(),
				model.TransferRequest{
					IdempotencyKey: fmt.Sprintf(
						"test-concurrent-%s-%d",
						uuid.New().String(),
						i,
					),
					FromWalletID: fromWallet,
					ToWalletID:   toWallet,
					Amount:       "10",
				},
			)

			errCh <- err
		}(i)
	}

	var errors []error

	for i := 0; i < transferCount; i++ {
		if err := <-errCh; err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		t.Fatalf(
			"%d of %d concurrent transfers failed: %v",
			len(errors),
			transferCount,
			errors,
		)
	}

	var fromBalance, toBalance int64

	err := db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		fromWallet,
	).Scan(&fromBalance)
	if err != nil {
		t.Fatal(err)
	}

	err = db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		toWallet,
	).Scan(&toBalance)
	if err != nil {
		t.Fatal(err)
	}

	if fromBalance != 0 {
		t.Fatalf("expected source balance 0, got %d", fromBalance)
	}

	if toBalance != 10000 {
		t.Fatalf("expected destination balance 10000, got %d", toBalance)
	}
}
