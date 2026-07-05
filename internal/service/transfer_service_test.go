package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/service"
)

func TestExecuteTransfer_Success(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	result, err := svc.ExecuteTransfer(ctx, service.TransferInput{
		IdempotencyKey: "key-success-1",
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         1000,
	})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	if result.Status != string(domain.TransferStatusProcessed) {
		t.Fatalf("expected status PROCESSED, got %s", result.Status)
	}

	var fromBalance, toBalance int64
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletA).Scan(&fromBalance); err != nil {
		t.Fatalf("query from wallet: %v", err)
	}
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletB).Scan(&toBalance); err != nil {
		t.Fatalf("query to wallet: %v", err)
	}

	if fromBalance != 99000 {
		t.Errorf("expected from wallet balance 99000, got %d", fromBalance)
	}
	if toBalance != 51000 {
		t.Errorf("expected to wallet balance 51000, got %d", toBalance)
	}

	var entryCount int
	if err := conn.QueryRow(`SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, result.TransferID).Scan(&entryCount); err != nil {
		t.Fatalf("query ledger entries: %v", err)
	}
	if entryCount != 2 {
		t.Errorf("expected exactly 2 ledger entries, got %d", entryCount)
	}

	var debitSum, creditSum int64
	err = conn.QueryRow(`
		SELECT
			COALESCE(SUM(amount) FILTER (WHERE type = 'DEBIT'), 0),
			COALESCE(SUM(amount) FILTER (WHERE type = 'CREDIT'), 0)
		FROM ledger_entries WHERE transfer_id = $1
	`, result.TransferID).Scan(&debitSum, &creditSum)
	if err != nil {
		t.Fatalf("query ledger sums: %v", err)
	}
	if debitSum != creditSum {
		t.Errorf("ledger does not balance: debit=%d credit=%d", debitSum, creditSum)
	}
}

func TestExecuteTransfer_IdempotentReplay(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	input := service.TransferInput{
		IdempotencyKey: "key-replay-1",
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         2000,
	}

	first, err := svc.ExecuteTransfer(ctx, input)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	second, err := svc.ExecuteTransfer(ctx, input)
	if err != nil {
		t.Fatalf("replay call failed: %v", err)
	}

	if first.TransferID != second.TransferID {
		t.Errorf("expected same transfer id on replay, got %s vs %s", first.TransferID, second.TransferID)
	}

	var transferCount int
	if err := conn.QueryRow(`SELECT count(*) FROM transfers`).Scan(&transferCount); err != nil {
		t.Fatalf("query transfer count: %v", err)
	}
	if transferCount != 1 {
		t.Errorf("expected exactly 1 transfer row after replay, got %d", transferCount)
	}

	var fromBalance int64
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletA).Scan(&fromBalance); err != nil {
		t.Fatalf("query from wallet: %v", err)
	}
	if fromBalance != 98000 {
		t.Errorf("expected balance debited only once (98000), got %d", fromBalance)
	}
}

func TestExecuteTransfer_ConflictingPayloadSameKey(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	_, err := svc.ExecuteTransfer(ctx, service.TransferInput{
		IdempotencyKey: "key-conflict-1",
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         1000,
	})
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}

	_, err = svc.ExecuteTransfer(ctx, service.TransferInput{
		IdempotencyKey: "key-conflict-1",
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         9999, // different amount, same key
	})
	if !errors.Is(err, domain.ErrIdempotencyKeyConflict) {
		t.Fatalf("expected ErrIdempotencyKeyConflict, got %v", err)
	}
}

func TestExecuteTransfer_InsufficientBalance_ResultsInFailedStatus(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	result, err := svc.ExecuteTransfer(ctx, service.TransferInput{
		IdempotencyKey: "key-insufficient-1",
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         999999, // exceeds seeded balance of 100000
	})
	if err != nil {
		t.Fatalf("expected no error (FAILED is a resolved outcome, not an error), got: %v", err)
	}

	if result.Status != string(domain.TransferStatusFailed) {
		t.Fatalf("expected status FAILED, got %s", result.Status)
	}
	if result.FailureReason == "" {
		t.Errorf("expected a failure reason to be set")
	}

	var fromBalance int64
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletA).Scan(&fromBalance); err != nil {
		t.Fatalf("query from wallet: %v", err)
	}
	if fromBalance != 100000 {
		t.Errorf("expected balance unchanged on failed transfer, got %d", fromBalance)
	}

	// Retry with the same key must return the same FAILED result, not re-attempt.
	retry, err := svc.ExecuteTransfer(ctx, service.TransferInput{
		IdempotencyKey: "key-insufficient-1",
		FromWalletID:   walletA,
		ToWalletID:     walletB,
		Amount:         999999,
	})
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if retry.TransferID != result.TransferID || retry.Status != string(domain.TransferStatusFailed) {
		t.Errorf("expected retry to return original FAILED result, got %+v", retry)
	}
}

func TestExecuteTransfer_SameWalletRejected(t *testing.T) {
	svc, conn := newTestService(t)
	resetTables(t, conn)
	ctx := context.Background()

	_, err := svc.ExecuteTransfer(ctx, service.TransferInput{
		IdempotencyKey: "key-same-wallet-1",
		FromWalletID:   walletA,
		ToWalletID:     walletA,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("expected ErrSameWallet, got %v", err)
	}
}
