package service

import (
	"context"
	"testing"

	"wallet-transfer-service/internal/repository"

	"github.com/google/uuid"
)

func TestCreateTransfer(t *testing.T) {

	conn := setupTestDB(t)

	defer conn.Close(context.Background())

	walletRepo := repository.NewWalletRepository(conn)

	transferRepo := repository.NewTransferRepository()

	ledgerRepo := repository.NewLedgerRepository()

	walletService := NewWalletService(
		conn,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	beforeFromWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	beforeToWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"22222222-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	transferAmount := int64(10)

	err = walletService.CreateTransfer(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		transferAmount,
		uuid.New().String(),
	)

	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	afterFromWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	afterToWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"22222222-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedFromBalance := beforeFromWallet.Balance - transferAmount
	expectedToBalance := beforeToWallet.Balance + transferAmount

	if afterFromWallet.Balance != expectedFromBalance {
		t.Fatalf(
			"expected sender balance %d, got %d",
			expectedFromBalance,
			afterFromWallet.Balance,
		)
	}

	if afterToWallet.Balance != expectedToBalance {
		t.Fatalf(
			"expected receiver balance %d, got %d",
			expectedToBalance,
			afterToWallet.Balance,
		)
	}
}

func TestCreateTransfer_Idempotency(t *testing.T) {

	conn := setupTestDB(t)

	defer conn.Close(context.Background())

	walletRepo := repository.NewWalletRepository(conn)

	transferRepo := repository.NewTransferRepository()

	ledgerRepo := repository.NewLedgerRepository()

	walletService := NewWalletService(
		conn,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	beforeFromWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	beforeToWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"22222222-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	transferAmount := int64(20)

	idempotencyKey := uuid.New().String()

	err = walletService.CreateTransfer(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		transferAmount,
		idempotencyKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	err = walletService.CreateTransfer(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		transferAmount,
		idempotencyKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	afterFromWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	afterToWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"22222222-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	expectedFromBalance := beforeFromWallet.Balance - transferAmount
	expectedToBalance := beforeToWallet.Balance + transferAmount

	if afterFromWallet.Balance != expectedFromBalance {
		t.Fatalf(
			"idempotency failed: expected sender balance %d, got %d",
			expectedFromBalance,
			afterFromWallet.Balance,
		)
	}

	if afterToWallet.Balance != expectedToBalance {
		t.Fatalf(
			"idempotency failed: expected receiver balance %d, got %d",
			expectedToBalance,
			afterToWallet.Balance,
		)
	}
}
func TestCreateTransfer_LedgerEntries(t *testing.T) {

	conn := setupTestDB(t)

	defer conn.Close(context.Background())

	walletRepo := repository.NewWalletRepository(conn)

	transferRepo := repository.NewTransferRepository()

	ledgerRepo := repository.NewLedgerRepository()

	walletService := NewWalletService(
		conn,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	idempotencyKey := uuid.New().String()

	err := walletService.CreateTransfer(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		15,
		idempotencyKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	defer tx.Rollback(context.Background())

	transfer, err := transferRepo.GetTransferByIdempotencyKey(
		context.Background(),
		tx,
		idempotencyKey,
	)
	if err != nil {
		t.Fatal(err)
	}

	count, err := ledgerRepo.CountLedgerEntriesByTransferID(
		context.Background(),
		conn,
		transfer.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if count != 2 {
		t.Fatalf(
			"expected 2 ledger entries, got %d",
			count,
		)
	}
}

func TestCreateTransfer_InsufficientBalance(t *testing.T) {

	conn := setupTestDB(t)

	defer conn.Close(context.Background())

	walletRepo := repository.NewWalletRepository(conn)

	transferRepo := repository.NewTransferRepository()

	ledgerRepo := repository.NewLedgerRepository()

	walletService := NewWalletService(
		conn,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	beforeFromWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	beforeToWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"22222222-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	err = walletService.CreateTransfer(
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		999999999,
		uuid.New().String(),
	)

	if err == nil {
		t.Fatal("expected insufficient balance error")
	}

	afterFromWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"11111111-1111-1111-1111-111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	afterToWallet, err := walletRepo.GetWalletByID(
		context.Background(),
		"22222222-2222-2222-2222-222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}

	if beforeFromWallet.Balance != afterFromWallet.Balance {
		t.Fatal("sender balance changed despite failed transfer")
	}

	if beforeToWallet.Balance != afterToWallet.Balance {
		t.Fatal("receiver balance changed despite failed transfer")
	}
}
