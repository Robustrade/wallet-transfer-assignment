package service

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type integrationTestSuite struct {
	db              *sql.DB
	transferService TransferService
}

type testWallets struct {
	fromWalletId string
	toWalletId   string
}

func setupIntegrationTest(t *testing.T) *integrationTestSuite {
	t.Helper()

	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("integration test disabled")
	}

	db, err := repository.NewPostgresDB("postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable")
	require.NoError(t, err)

	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()
	idempotencyRepo := repository.NewIdempotencyRepository()

	transferService := NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	suite := &integrationTestSuite{
		db:              db,
		transferService: transferService,
	}

	t.Cleanup(func() {
		db.Close()
	})

	return suite
}

func createTestWallets(t *testing.T, db *sql.DB) testWallets {
	t.Helper()

	ctx := context.Background()

	fromWalletId := uuid.New().String()
	toWalletId := uuid.New().String()

	_, err := db.ExecContext(ctx, `
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2), ($3, $4)
	`,
		fromWalletId, 10000,
		toWalletId, 5000,
	)
	require.NoError(t, err)

	return testWallets{
		fromWalletId: fromWalletId,
		toWalletId:   toWalletId,
	}
}

func cleanupTestData(t *testing.T, db *sql.DB, wallets testWallets, idempotencyKey string) {
	t.Helper()

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		DELETE FROM ledger_entries
		WHERE transfer_id IN (
			SELECT id FROM transfers
			WHERE idempotency_key = $1
		)
	`, idempotencyKey)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		DELETE FROM idempotency_records
		WHERE idempotency_key = $1
	`, idempotencyKey)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		DELETE FROM transfers
		WHERE idempotency_key = $1
	`, idempotencyKey)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `
		DELETE FROM wallets
		WHERE id IN ($1, $2)
	`, wallets.fromWalletId, wallets.toWalletId)
	require.NoError(t, err)
}

func TestCreateTransfer_Success(t *testing.T) {
	suite := setupIntegrationTest(t)
	wallets := createTestWallets(t, suite.db)
	idempotencyKey := "integration-success-" + uuid.New().String()

	t.Cleanup(func() {
		cleanupTestData(t, suite.db, wallets, idempotencyKey)
	})

	response, err := suite.transferService.CreateTransfer(
		context.Background(),
		domain.CreateTransferRequest{
			IdempotencyKey: idempotencyKey,
			FromWalletId:   wallets.fromWalletId,
			ToWalletId:     wallets.toWalletId,
			Amount:         2000,
		},
	)

	require.NoError(t, err)
	require.Equal(t, "PROCESSED", response.Status)
	require.Equal(t, int64(2000), response.Amount)

	var fromBalance int64
	var toBalance int64

	err = suite.db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		wallets.fromWalletId).Scan(&fromBalance)
	require.NoError(t, err)

	err = suite.db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		wallets.toWalletId).Scan(&toBalance)
	require.NoError(t, err)

	require.Equal(t, int64(8000), fromBalance)
	require.Equal(t, int64(7000), toBalance)
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	suite := setupIntegrationTest(t)
	wallets := createTestWallets(t, suite.db)
	idempotencyKey := "integration-idempotency-" + uuid.New().String()

	t.Cleanup(func() {
		cleanupTestData(t, suite.db, wallets, idempotencyKey)
	})

	req := domain.CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletId:   wallets.fromWalletId,
		ToWalletId:     wallets.toWalletId,
		Amount:         2000,
	}

	// First request.
	firstResponse, err := suite.transferService.CreateTransfer(context.Background(), req)

	require.NoError(t, err)
	require.Equal(t, "PROCESSED", firstResponse.Status)

	// Same request again.
	secondResponse, err := suite.transferService.CreateTransfer(context.Background(), req)

	require.NoError(t, err)
	require.Equal(t, "PROCESSED", secondResponse.Status)

	// Same transfer should be returned.
	require.Equal(t, firstResponse.TransferId, secondResponse.TransferId)

	// Money should only move once.
	var fromBalance int64
	var toBalance int64

	err = suite.db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		wallets.fromWalletId).Scan(&fromBalance)
	require.NoError(t, err)

	err = suite.db.QueryRow(
		`SELECT balance FROM wallets WHERE id = $1`,
		wallets.toWalletId).Scan(&toBalance)
	require.NoError(t, err)

	require.Equal(t, int64(8000), fromBalance)
	require.Equal(t, int64(7000), toBalance)
}

func TestCreateTransfer_Ledger(t *testing.T) {
	suite := setupIntegrationTest(t)
	wallets := createTestWallets(t, suite.db)
	idempotencyKey := "integration-ledger-" + uuid.New().String()

	var debitCount int
	var creditCount int

	t.Cleanup(func() {
		cleanupTestData(t, suite.db, wallets, idempotencyKey)
	})

	// Execute transfer.
	response, err := suite.transferService.CreateTransfer(
		context.Background(),
		domain.CreateTransferRequest{
			IdempotencyKey: idempotencyKey,
			FromWalletId:   wallets.fromWalletId,
			ToWalletId:     wallets.toWalletId,
			Amount:         2000,
		},
	)

	require.NoError(t, err)
	require.Equal(t, "PROCESSED", response.Status)

	// Verify ledger entries.
	rows, err := suite.db.Query(`
		SELECT wallet_id, entry_type, amount
		FROM ledger_entries
		WHERE transfer_id = $1
	`, response.TransferId)
	require.NoError(t, err)
	defer rows.Close()

	for rows.Next() {
		var walletId string
		var entryType string
		var amount int64

		err := rows.Scan(&walletId, &entryType, &amount)
		require.NoError(t, err)

		require.Equal(t, int64(2000), amount)

		switch entryType {
		case "DEBIT":
			debitCount++
			require.Equal(t, wallets.fromWalletId, walletId)

		case "CREDIT":
			creditCount++
			require.Equal(t, wallets.toWalletId, walletId)

		default:
			t.Fatalf("unexpected ledger entry type: %s", entryType)
		}
	}

	require.NoError(t, rows.Err())

	require.Equal(t, 1, debitCount)
	require.Equal(t, 1, creditCount)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	suite := setupIntegrationTest(t)
	wallets := createTestWallets(t, suite.db)
	idempotencyKey := "integration-insufficient-" + uuid.New().String()

	t.Cleanup(func() {
		cleanupTestData(t, suite.db, wallets, idempotencyKey)
	})

	response, err := suite.transferService.CreateTransfer(
		context.Background(),
		domain.CreateTransferRequest{
			IdempotencyKey: idempotencyKey,
			FromWalletId:   wallets.fromWalletId,
			ToWalletId:     wallets.toWalletId,
			Amount:         20000,
		},
	)

	// Service should return insufficient funds.
	require.ErrorIs(t, err, ErrInsufficientFunds)
	require.Nil(t, response)

	// Transfer should be stored as FAILED.
	var status string

	err = suite.db.QueryRow(`
		SELECT status
		FROM transfers
		WHERE idempotency_key = $1
	`, idempotencyKey).Scan(&status)

	require.NoError(t, err)
	require.Equal(t, "FAILED", status)

	// Source balance should remain unchanged.
	var fromBalance int64

	err = suite.db.QueryRow(`
		SELECT balance
		FROM wallets
		WHERE id = $1
	`, wallets.fromWalletId).Scan(&fromBalance)

	require.NoError(t, err)
	require.Equal(t, int64(10000), fromBalance)

	// Destination balance should remain unchanged.
	var toBalance int64

	err = suite.db.QueryRow(`
		SELECT balance
		FROM wallets
		WHERE id = $1
	`, wallets.toWalletId).Scan(&toBalance)

	require.NoError(t, err)
	require.Equal(t, int64(5000), toBalance)

	// No ledger entries should be created.
	var ledgerCount int

	err = suite.db.QueryRow(`
		SELECT COUNT(*)
		FROM ledger_entries
		WHERE transfer_id = (
			SELECT id
			FROM transfers
			WHERE idempotency_key = $1
		)
	`, idempotencyKey).Scan(&ledgerCount)

	require.NoError(t, err)
	require.Equal(t, 0, ledgerCount)
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	suite := setupIntegrationTest(t)
	toWalletId := uuid.New().String()
	fromWalletId := uuid.New().String()
	idempotencyKey := "integration-wallet-not-found-" + uuid.New().String()

	// Create only destination wallet.
	_, err := suite.db.ExecContext(
		context.Background(),
		`
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2)
		`,
		toWalletId,
		5000,
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = suite.db.ExecContext(
			context.Background(),
			`DELETE FROM wallets WHERE id = $1`,
			toWalletId,
		)
	})

	response, err := suite.transferService.CreateTransfer(
		context.Background(),
		domain.CreateTransferRequest{
			IdempotencyKey: idempotencyKey,
			FromWalletId:   fromWalletId,
			ToWalletId:     toWalletId,
			Amount:         2000,
		},
	)

	require.ErrorIs(t, err, ErrWalletNotFound)
	require.Nil(t, response)

	// Transfer creation should have been rolled back.
	var transferCount int

	err = suite.db.QueryRow(`
		SELECT COUNT(*)
		FROM transfers
		WHERE idempotency_key = $1
	`, idempotencyKey).Scan(&transferCount)

	require.NoError(t, err)
	require.Equal(t, 0, transferCount)
}

func TestCreateTransfer_ConcurrentTransfers(t *testing.T) {
	suite := setupIntegrationTest(t)
	wallets := createTestWallets(t, suite.db)
	key1 := "integration-concurrent-1-" + uuid.New().String()
	key2 := "integration-concurrent-2-" + uuid.New().String()

	t.Cleanup(func() {
		cleanupConcurrentTestData(t, suite.db, wallets, key1, key2)
	})

	req1 := domain.CreateTransferRequest{
		IdempotencyKey: key1,
		FromWalletId:   wallets.fromWalletId,
		ToWalletId:     wallets.toWalletId,
		Amount:         7000,
	}

	req2 := domain.CreateTransferRequest{
		IdempotencyKey: key2,
		FromWalletId:   wallets.fromWalletId,
		ToWalletId:     wallets.toWalletId,
		Amount:         7000,
	}

	type result struct {
		response *domain.CreateTransferResponse
		err      error
	}

	results := make(chan result, 2)

	go func() {
		response, err := suite.transferService.CreateTransfer(context.Background(), req1)
		results <- result{
			response: response,
			err:      err,
		}
	}()

	go func() {
		response, err := suite.transferService.CreateTransfer(context.Background(), req2)
		results <- result{
			response: response,
			err:      err,
		}
	}()

	first := <-results
	second := <-results

	resultsList := []result{first, second}

	var processedCount int
	var insufficientFundsCount int

	for _, result := range resultsList {
		if result.err == nil {
			require.NotNil(t, result.response)
			require.Equal(t, "PROCESSED", result.response.Status)
			processedCount++
			continue
		}

		if errors.Is(result.err, ErrInsufficientFunds) {
			insufficientFundsCount++
			continue
		}

		require.NoError(t, result.err)
	}

	require.Equal(t, 1, processedCount)
	require.Equal(t, 1, insufficientFundsCount)

	// Only 3,000 should remain in the source wallet.
	var fromBalance int64

	err := suite.db.QueryRow(`
		SELECT balance
		FROM wallets
		WHERE id = $1
	`, wallets.fromWalletId).Scan(&fromBalance)

	require.NoError(t, err)
	require.Equal(t, int64(3000), fromBalance)

	// Destination should receive exactly 7,000.
	var toBalance int64

	err = suite.db.QueryRow(`
		SELECT balance
		FROM wallets
		WHERE id = $1
	`, wallets.toWalletId).Scan(&toBalance)

	require.NoError(t, err)
	require.Equal(t, int64(12000), toBalance)
}

func cleanupConcurrentTestData(t *testing.T, db *sql.DB, wallets testWallets, key1 string, key2 string) {
	t.Helper()

	ctx := context.Background()

	// Delete ledger entries first.
	_, err := db.ExecContext(ctx, `
		DELETE FROM ledger_entries
		WHERE transfer_id IN (
			SELECT id
			FROM transfers
			WHERE idempotency_key IN ($1, $2)
		)
	`, key1, key2)
	require.NoError(t, err)

	// Delete idempotency records.
	_, err = db.ExecContext(ctx, `
		DELETE FROM idempotency_records
		WHERE idempotency_key IN ($1, $2)
	`, key1, key2)
	require.NoError(t, err)

	// Delete both transfers.
	_, err = db.ExecContext(ctx, `
		DELETE FROM transfers
		WHERE idempotency_key IN ($1, $2)
	`, key1, key2)
	require.NoError(t, err)

	// Now wallets can safely be deleted.
	_, err = db.ExecContext(ctx, `
		DELETE FROM wallets
		WHERE id IN ($1, $2)
	`, wallets.fromWalletId, wallets.toWalletId)
	require.NoError(t, err)
}
