package tests

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	db "github.com/mritunjaysingh22/wallet-transfer-assignment/internal/db"
	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ctx             = context.Background()
	testPool        *pgxpool.Pool
	testQueries     *db.Queries
	transferService *service.TransferService
)

func TestMain(m *testing.M) {

	// Load .env if present
	_ = godotenv.Load("../.env")

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"
	}

	var err error
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatalf("failed parsing database config: %v", err)
	}
	// Increase connection pool
	config.MaxConns = 50
	config.MinConns = 20

	testPool, err = pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}

	testQueries = db.New(testPool)

	transferService = service.NewTransferService(
		testPool,
		testQueries,
	)

	// Start every test with a clean database
	cleanupDatabase()

	code := m.Run()

	cleanupDatabase()

	testPool.Close()

	os.Exit(code)
}

func cleanupDatabase() {

	_, _ = testPool.Exec(ctx, "DELETE FROM ledger_entries")
	_, _ = testPool.Exec(ctx, "DELETE FROM transfers")
	_, _ = testPool.Exec(ctx, "DELETE FROM wallets")
}

func createWallet(
	t *testing.T,
	balance int64,
) string {

	t.Helper()

	id := uuid.New()

	var pgID pgtype.UUID
	copy(pgID.Bytes[:], id[:])
	pgID.Valid = true

	wallet, err := testQueries.CreateWallet(
		ctx,
		db.CreateWalletParams{
			ID:      pgID,
			Balance: balance,
		},
	)
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	return uuidString(wallet.ID)
}

func getWallet(
	t *testing.T,
	id string,
) db.Wallet {

	t.Helper()

	wallet, err := testQueries.GetWallet(
		ctx,
		mustUUID(id),
	)
	if err != nil {
		t.Fatalf("failed to get wallet: %v", err)
	}

	return wallet
}

func mustUUID(id string) pgtype.UUID {

	u, err := uuid.Parse(id)
	if err != nil {
		panic(err)
	}

	var pgID pgtype.UUID
	copy(pgID.Bytes[:], u[:])
	pgID.Valid = true

	return pgID
}

func uuidString(id pgtype.UUID) string {

	if !id.Valid {
		return ""
	}

	u, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		panic(err)
	}

	return u.String()
}
