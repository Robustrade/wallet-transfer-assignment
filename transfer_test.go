package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

const testDB = "postgres://wallet_user:wallet_pass@localhost:5432/wallet?sslmode=disable"

func setupDB(t *testing.T) *sql.DB {
	db, err := sql.Open("postgres", testDB)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Cleaning database...")

	_, err = db.Exec(`
		DELETE FROM ledger_entries;
		DELETE FROM transfers;
		DELETE FROM idempotency_records;
		DELETE FROM wallets;
	`)
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Seeding wallets...")

	_, err = db.Exec(`
		INSERT INTO wallets (id, balance) VALUES 
		('wallet_1', 900),
		('wallet_2', 0)
	`)
	if err != nil {
		t.Fatal(err)
	}

	return db
}

func TestConcurrentTransfers(t *testing.T) {
	t.Log("Starting TestConcurrentTransfers")

	db := setupDB(t)
	defer db.Close()

	var wg sync.WaitGroup
	numRequests := 5
	amount := 300

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	t.Logf("Sending %d concurrent requests with amount=%d", numRequests, amount)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			body := []byte(fmt.Sprintf(`{
				"idempotencyKey":"concurrent-%d",
				"fromWalletId":"wallet_1",
				"toWalletId":"wallet_2",
				"amount":%d
			}`, i, amount))

			req, err := http.NewRequestWithContext(context.Background(),
				"POST",
				"http://localhost:8080/transfers",
				bytes.NewBuffer(body),
			)
			if err != nil {
				t.Error(err)
				return
			}

			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			defer resp.Body.Close()

			t.Logf("Request %d completed with status %d", i, resp.StatusCode)
		}(i)
	}

	wg.Wait()

	t.Log("All goroutines completed. Running assertions...")

	// 1. Balance should never go negative
	var balance int64
	err := db.QueryRow(`SELECT balance FROM wallets WHERE id='wallet_1'`).Scan(&balance)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Final balance of wallet_1: %d", balance)

	if balance < 0 {
		t.Fatalf("balance went negative: %d", balance)
	}

	// 2. Only 3 transfers should succeed (900 / 300)
	var successCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM transfers WHERE status='PROCESSED'
	`).Scan(&successCount)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Successful transfers: %d", successCount)

	if successCount != 3 {
		t.Fatalf("expected 3 successful transfers, got %d", successCount)
	}

	// 3. Ledger must balance
	var debit, credit int64
	err = db.QueryRow(`
		SELECT 
		  COALESCE(SUM(CASE WHEN type='DEBIT' THEN amount END),0),
		  COALESCE(SUM(CASE WHEN type='CREDIT' THEN amount END),0)
		FROM ledger_entries
	`).Scan(&debit, &credit)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Ledger totals → debit: %d, credit: %d", debit, credit)

	if debit != credit {
		t.Fatalf("ledger mismatch: debit=%d credit=%d", debit, credit)
	}

	t.Log("TestConcurrentTransfers PASSED")
}

func TestIdempotency(t *testing.T) {
	t.Log("Starting TestIdempotency")

	db := setupDB(t)
	defer db.Close()

	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	body := []byte(`{
		"idempotencyKey":"same-key",
		"fromWalletId":"wallet_1",
		"toWalletId":"wallet_2",
		"amount":100
	}`)

	t.Log("Sending same request twice with identical idempotency key")

	for i := 0; i < 2; i++ {
		resp, err := client.Post(
			"http://localhost:8080/transfers",
			"application/json",
			bytes.NewBuffer(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		t.Logf("Request %d response status: %d", i+1, resp.StatusCode)
	}

	// only ONE transfer should exist
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM transfers`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Total transfers in DB: %d", count)

	if count != 1 {
		t.Fatalf("expected 1 transfer, got %d", count)
	}

	t.Log("TestIdempotency PASSED")
}

func TestInsufficientFunds(t *testing.T) {
	db := setupDB(t)
	defer db.Close()

	client := &http.Client{}

	body := []byte(`{
        "idempotencyKey":"fail-test",
        "fromWalletId":"wallet_1",
        "toWalletId":"wallet_2",
        "amount":1000
    }`)

	resp, err := client.Post(
		"http://localhost:8080/transfers",
		"application/json",
		bytes.NewBuffer(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var count int
	_ = db.QueryRow(`SELECT COUNT(*) FROM transfers WHERE status='FAILED'`).Scan(&count)

	if count != 1 {
		t.Fatalf("expected 1 failed transfer, got %d", count)
	}

}
