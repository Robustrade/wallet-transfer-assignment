package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	dbPool *pgxpool.Pool
	svcURL = "http://localhost:8080"
)

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// Integration tests require an existing docker-compose with DATABASE_URL set.
		// If not set, skip the suite.
		println("DATABASE_URL not set; using default")
		dbURL = "postgres://postgres:Test001@localhost:5432/wallets?sslmode=disable"
	}

	ctx := context.Background()
	var err error
	dbPool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Printf("failed to connect to db: %v\n", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	code := m.Run()
	os.Exit(code)
}

// helpers
type createWalletReq struct {
	WalletId       string `json:"walletId"`
	InitialBalance int64  `json:"initialBalance"`
}

type transferReq struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletId   string `json:"fromWalletId"`
	ToWalletId     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResp struct {
	Status        string `json:"status"`
	TransactionId int64  `json:"transactionId"`
}

type walletResp struct {
	WalletId int64 `json:"walletId"`
	Balance  int64 `json:"balance"`
}

func postJSON(t *testing.T, url string, body any) (*http.Response, []byte) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("http post failed: %v", err)
	}
	defer func() {}()
	data := make([]byte, 0)
	if resp.Body != nil {
		defer resp.Body.Close()
		data, _ = ioReadAll(resp.Body)
	}
	return resp, data
}

func getJSON(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("http get failed: %v", err)
	}
	defer func() {}()
	data := make([]byte, 0)
	if resp.Body != nil {
		defer resp.Body.Close()
		data, _ = ioReadAll(resp.Body)
	}
	return resp, data
}

// minimal io.ReadAll replacement to avoid importing ioutil for older go versions
func ioReadAll(r io.ReadCloser) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}

// cleanup created rows (wallets, their transactions and ledger entries). Accepts wallet ids.
func cleanupWallets(t *testing.T, ids []int64) {
	t.Helper()
	if len(ids) == 0 {
		return
	}
	ctx := context.Background()
	// Build unique numbered placeholders for each occurrence to match argument positions
	n := len(ids)
	args := make([]any, 2*n)
	placeholders1 := ""
	placeholders2 := ""
	for i, id := range ids {
		args[i] = id
		args[n+i] = id
		if i > 0 {
			placeholders1 += ","
			placeholders2 += ","
		}
		placeholders1 += "$" + strconv.Itoa(i+1)
		placeholders2 += "$" + strconv.Itoa(n+i+1)
	}

	q1 := fmt.Sprintf(`DELETE FROM ledger_entries WHERE transaction_id IN (SELECT id FROM transactions WHERE from_wallet_id IN (%s) OR to_wallet_id IN (%s))`, placeholders1, placeholders2)
	if _, err := dbPool.Exec(ctx, q1, args...); err != nil {
		t.Logf("cleanup ledger_entries by transaction failed: %v", err)
	}

	q2 := fmt.Sprintf(`DELETE FROM ledger_entries WHERE wallet_id IN (%s)`, placeholders1)
	if _, err := dbPool.Exec(ctx, q2, args[:n]...); err != nil {
		t.Logf("cleanup ledger_entries by wallet failed: %v", err)
	}

	q3 := fmt.Sprintf(`DELETE FROM transactions WHERE from_wallet_id IN (%s) OR to_wallet_id IN (%s)`, placeholders1, placeholders2)
	if _, err := dbPool.Exec(ctx, q3, args...); err != nil {
		t.Logf("cleanup transactions failed: %v", err)
	}

	q4 := fmt.Sprintf(`DELETE FROM wallets WHERE wallet_id IN (%s)`, placeholders1)
	if _, err := dbPool.Exec(ctx, q4, args[:n]...); err != nil {
		t.Logf("cleanup wallets failed: %v", err)
	}
}

func createWallet(t *testing.T, id string, balance int64) {
	t.Helper()
	req := createWalletReq{WalletId: id, InitialBalance: balance}
	resp, data := postJSON(t, svcURL+"/wallets", req)
	if resp.StatusCode != 201 {
		t.Fatalf("create wallet failed: status=%d body=%s", resp.StatusCode, string(data))
	}
}

func getWalletBalance(t *testing.T, id string) int64 {
	t.Helper()
	resp, data := getJSON(t, svcURL+"/wallets/"+id)
	if resp.StatusCode != 200 {
		t.Fatalf("get wallet failed: status=%d body=%s", resp.StatusCode, string(data))
	}
	var w walletResp
	if err := json.Unmarshal(data, &w); err != nil {
		t.Fatalf("invalid wallet response: %v", err)
	}
	return w.Balance
}

func postTransfer(t *testing.T, idempotencyKey, from, to string, amount int64) (transferResp, int) {
	t.Helper()
	req := transferReq{IdempotencyKey: idempotencyKey, FromWalletId: from, ToWalletId: to, Amount: amount}
	resp, data := postJSON(t, svcURL+"/transfers", req)
	var tr transferResp
	if len(data) > 0 {
		_ = json.Unmarshal(data, &tr)
	}
	return tr, resp.StatusCode
}

// Tests
func TestHappyPathTransfer(t *testing.T) {
	fromID := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
	toID := fromID + "2"
	created := []int64{}
	defer func() {
		cleanupWallets(t, created)
	}()

	// create wallets
	createWallet(t, fromID, 1000)
	createWallet(t, toID, 500)
	fid, _ := strconv.ParseInt(fromID, 10, 64)
	tid, _ := strconv.ParseInt(toID, 10, 64)
	created = append(created, fid, tid)

	// perform transfer
	key := fmt.Sprintf("k-%d", time.Now().UnixNano())
	tr, status := postTransfer(t, key, fromID, toID, 200)
	if status != 200 {
		t.Fatalf("transfer failed status=%d", status)
	}
	if tr.Status != "PROCESSED" {
		t.Fatalf("expected PROCESSED got %s", tr.Status)
	}

	// verify balances
	fromBal := getWalletBalance(t, fromID)
	toBal := getWalletBalance(t, toID)
	if fromBal != 800 {
		t.Fatalf("unexpected from balance: %d", fromBal)
	}
	if toBal != 700 {
		t.Fatalf("unexpected to balance: %d", toBal)
	}

	// verify ledger entries count = 2
	ctx := context.Background()
	var cnt int
	err := dbPool.QueryRow(ctx, "SELECT count(1) FROM ledger_entries WHERE transaction_id=$1", tr.TransactionId).Scan(&cnt)
	if err != nil {
		t.Fatalf("query ledger entries failed: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", cnt)
	}
}

func TestIdempotentRetry(t *testing.T) {
	fromID := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
	toID := fromID + "3"
	created := []int64{}
	defer func() { cleanupWallets(t, created) }()

	createWallet(t, fromID, 1000)
	createWallet(t, toID, 0)
	fid, _ := strconv.ParseInt(fromID, 10, 64)
	tid, _ := strconv.ParseInt(toID, 10, 64)
	created = append(created, fid, tid)

	key := fmt.Sprintf("idem-%d", time.Now().UnixNano())
	tr1, s1 := postTransfer(t, key, fromID, toID, 100)
	if s1 != 200 || tr1.Status != "PROCESSED" {
		t.Fatalf("first transfer failed: status=%d resp=%+v", s1, tr1)
	}
	tr2, s2 := postTransfer(t, key, fromID, toID, 100)
	if s2 != 200 {
		t.Fatalf("retry failed: status=%d", s2)
	}
	if tr1.TransactionId != tr2.TransactionId {
		t.Fatalf("idempotent retry returned different transaction ids: %d vs %d", tr1.TransactionId, tr2.TransactionId)
	}

	// ensure just two ledger entries exist for that transaction
	ctx := context.Background()
	var cnt int
	if err := dbPool.QueryRow(ctx, "SELECT count(1) FROM ledger_entries WHERE transaction_id=$1", tr1.TransactionId).Scan(&cnt); err != nil {
		t.Fatalf("query ledger entries failed: %v", err)
	}
	if cnt != 2 {
		t.Fatalf("expected 2 ledger entries after retry, got %d", cnt)
	}
}

func TestInsufficientFunds(t *testing.T) {
	fromID := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
	toID := fromID + "4"
	created := []int64{}
	defer func() { cleanupWallets(t, created) }()

	createWallet(t, fromID, 50)
	createWallet(t, toID, 0)
	fid, _ := strconv.ParseInt(fromID, 10, 64)
	tid, _ := strconv.ParseInt(toID, 10, 64)
	created = append(created, fid, tid)

	key := fmt.Sprintf("insuf-%d", time.Now().UnixNano())
	_, status := postTransfer(t, key, fromID, toID, 100)
	if status == 200 {
		t.Fatalf("expected transfer to fail due to insufficient funds")
	}

	// balances unchanged
	if b := getWalletBalance(t, fromID); b != 50 {
		t.Fatalf("from balance changed unexpectedly: %d", b)
	}
	if b := getWalletBalance(t, toID); b != 0 {
		t.Fatalf("to balance changed unexpectedly: %d", b)
	}
}

func TestConcurrentContention(t *testing.T) {
	fromID := strconv.FormatInt(time.Now().UnixNano()/1e6, 10)
	created := []int64{}
	defer func() { cleanupWallets(t, created) }()

	// create one source wallet and 10 destination wallets
	createWallet(t, fromID, 1000)
	fid, _ := strconv.ParseInt(fromID, 10, 64)
	created = append(created, fid)

	destIDs := make([]string, 10)
	for i := 0; i < 10; i++ {
		// ensure numeric wallet ids by appending a digit suffix
		id := fmt.Sprintf("%s%d", fromID, i+1)
		createWallet(t, id, 0)
		di, _ := strconv.ParseInt(id, 10, 64)
		created = append(created, di)
		destIDs[i] = id
	}

	// run 10 concurrent transfers of 100 each
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(dest string, idx int) {
			defer wg.Done()
			key := fmt.Sprintf("con-%d-%d", idx, time.Now().UnixNano())
			tr, status := postTransfer(t, key, fromID, dest, 100)
			if status != 200 || tr.Status != "PROCESSED" {
				t.Errorf("concurrent transfer failed for %s: status=%d resp=%+v", dest, status, tr)
			}
		}(destIDs[i], i)
	}
	wg.Wait()

	// final balance should be 0
	if b := getWalletBalance(t, fromID); b != 0 {
		t.Fatalf("expected final balance 0, got %d", b)
	}
}
