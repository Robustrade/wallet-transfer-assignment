package integration

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"wallet-transfer-assignment/ent"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/entrepo"
	"wallet-transfer-assignment/internal/service"
)

func TestConcurrentTransfers_PreventsDoubleSpend(t *testing.T) {
	ctx := context.Background()
	client := openPostgresTestClient(t)

	cleanDatabase(t, ctx, client)

	// Source has only enough money for ONE of these transfers.
	sourceID := uuid.New()
	destinationAID := uuid.New()
	destinationBID := uuid.New()

	_, err := client.Wallet.Create().
		SetID(sourceID).
		SetBalance(100).
		Save(ctx)
	if err != nil {
		t.Fatalf("create source wallet: %v", err)
	}

	_, err = client.Wallet.Create().
		SetID(destinationAID).
		SetBalance(0).
		Save(ctx)
	if err != nil {
		t.Fatalf("create destination A: %v", err)
	}

	_, err = client.Wallet.Create().
		SetID(destinationBID).
		SetBalance(0).
		Save(ctx)
	if err != nil {
		t.Fatalf("create destination B: %v", err)
	}

	txManager := entrepo.NewTransactionManager(client)
	transferRepo := entrepo.NewTransferRepository(client)

	svc := service.NewTransferService(
		txManager,
		transferRepo,
	)

	requests := []domain.CreateTransfer{
		{
			IdempotencyKey: "concurrent-transfer-A",
			FromWalletID:   sourceID,
			ToWalletID:     destinationAID,
			Amount:         80,
		},
		{
			IdempotencyKey: "concurrent-transfer-B",
			FromWalletID:   sourceID,
			ToWalletID:     destinationBID,
			Amount:         80,
		},
	}

	type result struct {
		result *service.CreateTransferResult
		err    error
	}

	results := make(chan result, 2)

	var wg sync.WaitGroup

	start := make(chan struct{})

	for _, req := range requests {
		req := req

		wg.Add(1)

		go func() {
			defer wg.Done()

			// Make both goroutines begin as close together
			// as possible.
			<-start

			r, err := svc.CreateTransfer(ctx, req)

			results <- result{
				result: r,
				err:    err,
			}
		}()
	}

	close(start)

	wg.Wait()
	close(results)

	var successCount int
	var insufficientFundsCount int

	for r := range results {
		switch {
		case r.err == nil:
			successCount++

			if r.result == nil ||
				r.result.Transfer == nil {

				t.Fatal("successful transfer returned nil result")
			}

			if r.result.Transfer.Status !=
				domain.TransferStatusProcessed {

				t.Fatalf(
					"expected PROCESSED transfer, got %s",
					r.result.Transfer.Status,
				)
			}

		case errors.Is(
			r.err,
			domain.ErrInsufficientFunds,
		):
			insufficientFundsCount++

			if r.result == nil ||
				r.result.Transfer == nil {

				t.Fatal("failed transfer should still return transfer result")
			}

			if r.result.Transfer.Status !=
				domain.TransferStatusFailed {

				t.Fatalf(
					"expected FAILED transfer, got %s",
					r.result.Transfer.Status,
				)
			}

		default:
			t.Fatalf(
				"unexpected concurrent transfer error: %v",
				r.err,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"expected exactly 1 successful transfer, got %d",
			successCount,
		)
	}

	if insufficientFundsCount != 1 {
		t.Fatalf(
			"expected exactly 1 insufficient-funds transfer, got %d",
			insufficientFundsCount,
		)
	}

	// -----------------------------------------------------------------
	// Verify final balances directly from PostgreSQL.
	// -----------------------------------------------------------------

	source, err := client.Wallet.Get(ctx, sourceID)
	if err != nil {
		t.Fatalf("query source wallet: %v", err)
	}

	destinationA, err := client.Wallet.Get(
		ctx,
		destinationAID,
	)
	if err != nil {
		t.Fatalf("query destination A: %v", err)
	}

	destinationB, err := client.Wallet.Get(
		ctx,
		destinationBID,
	)
	if err != nil {
		t.Fatalf("query destination B: %v", err)
	}

	// Only ONE transfer of 80 may succeed.
	if source.Balance != 20 {
		t.Fatalf(
			"expected source balance 20, got %d",
			source.Balance,
		)
	}

	totalDestinationBalance :=
		destinationA.Balance + destinationB.Balance

	if totalDestinationBalance != 80 {
		t.Fatalf(
			"expected total destination balance 80, got %d",
			totalDestinationBalance,
		)
	}

	// -----------------------------------------------------------------
	// Ledger must contain exactly one DEBIT + one CREDIT.
	// -----------------------------------------------------------------

	ledgerEntries, err := client.LedgerEntry.
		Query().
		All(ctx)

	if err != nil {
		t.Fatalf("query ledger entries: %v", err)
	}

	if len(ledgerEntries) != 2 {
		t.Fatalf(
			"expected exactly 2 ledger entries, got %d",
			len(ledgerEntries),
		)
	}

	// -----------------------------------------------------------------
	// Both attempts are persisted:
	//
	// 1 PROCESSED
	// 1 FAILED
	// -----------------------------------------------------------------

	transfers, err := client.Transfer.
		Query().
		All(ctx)

	if err != nil {
		t.Fatalf("query transfers: %v", err)
	}

	if len(transfers) != 2 {
		t.Fatalf(
			"expected 2 transfer records, got %d",
			len(transfers),
		)
	}

	var processed int
	var failed int

	for _, tr := range transfers {
		switch string(tr.Status) {
		case "PROCESSED":
			processed++
		case "FAILED":
			failed++
		}
	}

	if processed != 1 {
		t.Fatalf(
			"expected 1 PROCESSED transfer, got %d",
			processed,
		)
	}

	if failed != 1 {
		t.Fatalf(
			"expected 1 FAILED transfer, got %d",
			failed,
		)
	}
}

func openPostgresTestClient(t *testing.T) *ent.Client {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")

	if databaseURL == "" {
		t.Skip(
			"TEST_DATABASE_URL is not set; skipping PostgreSQL integration test",
		)
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf(
			"open PostgreSQL connection: %v",
			err,
		)
	}

	// Important: concurrency test requires more than one DB connection.
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		t.Fatalf(
			"ping PostgreSQL: %v",
			err,
		)
	}

	driver := entsql.OpenDB(
		dialect.Postgres,
		db,
	)

	client := ent.NewClient(
		ent.Driver(driver),
	)

	t.Cleanup(func() {
		_ = client.Close()
	})

	// For integration tests, auto-create the schema.
	// Ent supports Schema.Create for this purpose.
	if err := client.Schema.Create(
		context.Background(),
	); err != nil {
		t.Fatalf(
			"create PostgreSQL test schema: %v",
			err,
		)
	}

	return client
}

func cleanDatabase(
	t *testing.T,
	ctx context.Context,
	client *ent.Client,
) {
	t.Helper()

	// FK-safe deletion order:
	// ledger_entries -> transfers -> wallets

	if _, err := client.LedgerEntry.
		Delete().
		Exec(ctx); err != nil {

		t.Fatalf(
			"clean ledger entries: %v",
			err,
		)
	}

	if _, err := client.Transfer.
		Delete().
		Exec(ctx); err != nil {

		t.Fatalf(
			"clean transfers: %v",
			err,
		)
	}

	if _, err := client.Wallet.
		Delete().
		Exec(ctx); err != nil {

		t.Fatalf(
			"clean wallets: %v",
			err,
		)
	}
}
