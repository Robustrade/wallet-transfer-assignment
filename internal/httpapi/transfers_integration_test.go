package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/robustrade/wallet-transfer/internal/config"
	"github.com/robustrade/wallet-transfer/internal/domain"
	"github.com/robustrade/wallet-transfer/internal/service"
	"github.com/robustrade/wallet-transfer/internal/store"
)

func TestTransferAPI_Postgres(t *testing.T) {
	if envFile := os.Getenv("TEST_ENV_FILE"); envFile != "" {
		if err := config.LoadEnvFile(envFile); err != nil {
			t.Fatal(err)
		}
	}
	explicitURL := firstNonEmpty(os.Getenv("TEST_DATABASE_URL"), os.Getenv("DB_URL"), os.Getenv("DATABASE_URL"))
	dsn, err := config.DatabaseURL(explicitURL, os.Getenv("DB_HOST"), os.Getenv("DB_PORT"), os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME"), os.Getenv("DB_SSL_MODE"))
	if err != nil {
		t.Fatal(err)
	}
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL or DATABASE_URL, or configure DB_HOST, DB_USER, and DB_NAME (optionally via TEST_ENV_FILE)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schemaName := "wallet_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schemaName)); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", schemaName))
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := store.NewPostgres(pool)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, wallet := range []domain.Wallet{{ID: "alice", Balance: 100}, {ID: "bob", Balance: 0}, {ID: "carol", Balance: 100}} {
		if _, err := pool.Exec(ctx, `INSERT INTO wallets (id, balance) VALUES ($1, $2)`, wallet.ID, wallet.Balance); err != nil {
			t.Fatal(err)
		}
	}

	e := echo.New()
	NewHandler(service.NewTransfers(repo)).Register(e)
	request := func(key, from, to string, amount int64) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(domain.TransferRequest{IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount})
		req := httptest.NewRequest(http.MethodPost, "/transfers", bytes.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		response := httptest.NewRecorder()
		e.ServeHTTP(response, req)
		return response
	}

	first := request("key-1", "alice", "bob", 40)
	if first.Code != http.StatusCreated {
		t.Fatalf("first transfer status = %d, body=%s", first.Code, first.Body)
	}
	var processedTransfer struct {
		Transfer domain.Transfer `json:"transfer"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &processedTransfer); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE transfers SET status='FAILED' WHERE id=$1`, processedTransfer.Transfer.ID); err == nil {
		t.Fatal("processed transfer was allowed to transition to FAILED")
	}
	var persistedStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM transfers WHERE id=$1`, processedTransfer.Transfer.ID).Scan(&persistedStatus); err != nil {
		t.Fatal(err)
	}
	if persistedStatus != "PROCESSED" {
		t.Fatalf("transfer status after rejected transition = %q, want PROCESSED", persistedStatus)
	}
	for _, mutation := range []struct {
		field string
		query string
	}{
		{field: "from_wallet_id", query: `UPDATE transfers SET from_wallet_id='carol' WHERE id=$1`},
		{field: "to_wallet_id", query: `UPDATE transfers SET to_wallet_id='carol' WHERE id=$1`},
		{field: "amount", query: `UPDATE transfers SET amount=39 WHERE id=$1`},
	} {
		if _, err := pool.Exec(ctx, mutation.query, processedTransfer.Transfer.ID); err == nil {
			t.Errorf("processed transfer allowed update of %s", mutation.field)
		}
	}
	var fromWalletID, toWalletID string
	var persistedAmount int64
	if err := pool.QueryRow(ctx, `SELECT from_wallet_id, to_wallet_id, amount FROM transfers WHERE id=$1`, processedTransfer.Transfer.ID).Scan(&fromWalletID, &toWalletID, &persistedAmount); err != nil {
		t.Fatal(err)
	}
	if fromWalletID != "alice" || toWalletID != "bob" || persistedAmount != 40 {
		t.Fatalf("transfer fields after rejected updates = (%q, %q, %d), want (alice, bob, 40)", fromWalletID, toWalletID, persistedAmount)
	}
	for _, scenario := range []struct {
		name         string
		debitWallet  string
		creditWallet string
		debitAmount  int64
		creditAmount int64
	}{
		{name: "amount mismatch", debitWallet: "alice", creditWallet: "bob", debitAmount: 39, creditAmount: 39},
		{name: "debit wallet mismatch", debitWallet: "carol", creditWallet: "bob", debitAmount: 40, creditAmount: 40},
		{name: "credit wallet mismatch", debitWallet: "alice", creditWallet: "carol", debitAmount: 40, creditAmount: 40},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)

			id := uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status) VALUES ($1, 'alice', 'bob', 40, 'PENDING')`, id); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount) VALUES ($1, $2, 'DEBIT', $3), ($4, $2, 'CREDIT', $5)`, scenario.debitWallet, id, scenario.debitAmount, scenario.creditWallet, scenario.creditAmount); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `UPDATE transfers SET status = 'PROCESSED' WHERE id = $1`, id); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(ctx); err == nil {
				t.Errorf("malformed ledger was allowed to commit")
			}
		})
	}
	missingWallet := request("key-missing-wallet", "missing", "bob", 1)
	var missingWalletResponse errorResponse
	if missingWallet.Code != http.StatusNotFound || json.Unmarshal(missingWallet.Body.Bytes(), &missingWalletResponse) != nil || missingWalletResponse.Error.Code != ErrCodeWalletNotFound {
		t.Fatalf("missing wallet response = (%d, %s), want 404 wallet_not_found", missingWallet.Code, missingWallet.Body)
	}
	replay := request("key-1", "alice", "bob", 40)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = (%d, %s), want original (%d, %s)", replay.Code, replay.Body, first.Code, first.Body)
	}
	if conflict := request("key-1", "alice", "bob", 41); conflict.Code != http.StatusConflict {
		t.Fatalf("key conflict status = %d", conflict.Code)
	}
	var transfer domain.Transfer
	if err := json.Unmarshal(first.Body.Bytes(), &struct {
		Transfer *domain.Transfer `json:"transfer"`
	}{Transfer: &transfer}); err != nil {
		t.Fatal(err)
	}
	var debitCount int
	var debit, credit int64
	err = pool.QueryRow(ctx, `SELECT count(*), sum(CASE WHEN type='DEBIT' THEN amount ELSE 0 END), sum(CASE WHEN type='CREDIT' THEN amount ELSE 0 END) FROM ledger_entries WHERE transfer_id=$1`, transfer.ID).Scan(&debitCount, &debit, &credit)
	if err != nil {
		t.Fatal(err)
	}
	if debitCount != 2 || debit != 40 || credit != 40 {
		t.Fatalf("ledger: count=%d debit=%d credit=%d", debitCount, debit, credit)
	}
	var aliceBalance, bobBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id='alice'`).Scan(&aliceBalance); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id='bob'`).Scan(&bobBalance); err != nil {
		t.Fatal(err)
	}
	if aliceBalance != 60 || bobBalance != 40 {
		t.Fatalf("balances alice=%d bob=%d", aliceBalance, bobBalance)
	}

	failed := request("key-fail", "alice", "bob", 100)
	if failed.Code != http.StatusUnprocessableEntity {
		t.Fatalf("insufficient funds status = %d", failed.Code)
	}
	if again := request("key-fail", "alice", "bob", 100); again.Code != failed.Code || again.Body.String() != failed.Body.String() {
		t.Fatalf("failed request was not replayed")
	}
	if _, err := pool.Exec(ctx, `UPDATE wallets SET balance=200 WHERE id='alice'`); err != nil {
		t.Fatal(err)
	}
	if stillFailed := request("key-fail", "alice", "bob", 100); stillFailed.Code != failed.Code || stillFailed.Body.String() != failed.Body.String() {
		t.Fatalf("terminal failure changed after balance update")
	}

	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			statuses <- request(fmt.Sprintf("concurrent-%d", i), "carol", "bob", 80).Code
		}(i)
	}
	wg.Wait()
	close(statuses)
	created, unprocessable := 0, 0
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		}
		if status == http.StatusUnprocessableEntity {
			unprocessable++
		}
	}
	if created != 1 || unprocessable != 1 {
		t.Fatalf("concurrent transfer statuses: created=%d insufficient=%d", created, unprocessable)
	}
	var carolBalance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id='carol'`).Scan(&carolBalance); err != nil {
		t.Fatal(err)
	}
	if carolBalance != 20 {
		t.Fatalf("concurrent debit balance = %d, want 20", carolBalance)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
