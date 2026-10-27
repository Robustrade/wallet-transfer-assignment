package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-service/internal/database"
	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
	"wallet-service/internal/testutil"
)

func request(key string, amount int64) domain.Request {
	return domain.Request{IdempotencyKey: key, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: amount}
}

func assertResult(t *testing.T, result repository.Result, httpStatus int, status domain.Status, code string) {
	t.Helper()
	var body struct {
		TransferID string        `json:"transferId"`
		Status     domain.Status `json:"status"`
		Error      *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.Body), &body); err != nil {
		t.Fatal(err)
	}
	gotCode := ""
	if body.Error != nil {
		gotCode = body.Error.Code
	}
	if result.HTTPStatus != httpStatus || body.Status != status || gotCode != code || body.TransferID != result.TransferID || (status != "") != (body.TransferID != "") {
		t.Fatalf("unexpected response: %+v", result)
	}
}

func assertState(t *testing.T, pool *pgxpool.Pool, from, to int64, processed, failed, keys int) {
	t.Helper()
	var a, b int64
	var p, f, l, k, incomplete, invalidLedger int
	err := pool.QueryRow(t.Context(), `SELECT
		(SELECT balance_minor FROM wallets WHERE id='wallet_1'),
		(SELECT balance_minor FROM wallets WHERE id='wallet_2'),
		(SELECT count(*) FROM transfers WHERE status='PROCESSED'),
		(SELECT count(*) FROM transfers WHERE status='FAILED'),
		(SELECT count(*) FROM ledger_entries),
		(SELECT count(*) FROM idempotency_records),
		(SELECT count(*) FROM transfers WHERE status='PENDING') +
		(SELECT count(*) FROM idempotency_records WHERE http_status IS NULL OR response_body IS NULL),
		(SELECT count(*) FROM ledger_entries e JOIN transfers t ON t.id=e.transfer_id
		 WHERE t.status <> 'PROCESSED' OR e.amount_minor <> t.amount_minor OR
		 e.wallet_id <> CASE e.entry_type WHEN 'DEBIT' THEN t.from_wallet_id ELSE t.to_wallet_id END)`).
		Scan(&a, &b, &p, &f, &l, &k, &incomplete, &invalidLedger)
	if err != nil {
		t.Fatal(err)
	}
	if a != from || b != to || p != processed || f != failed || l != 2*processed || k != keys || incomplete != 0 || invalidLedger != 0 {
		t.Fatalf("state: balances=%d/%d processed=%d failed=%d ledger=%d keys=%d incomplete=%d invalidLedger=%d", a, b, p, f, l, k, incomplete, invalidLedger)
	}
}

func execSQL(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql); err != nil {
		t.Fatal(err)
	}
}

func TestTransferSuccessReplayAndConflict(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	svc := New(pool)
	req := request("success", 100)
	result, err := svc.Transfer(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	assertResult(t, result, 201, domain.Processed, "")
	assertState(t, pool, 400, 300, 1, 0, 1)
	// A fresh service must replay the original serialized response exactly.
	replay, err := New(pool).Transfer(t.Context(), req)
	if err != nil || replay != result {
		t.Fatalf("replay: %+v, %v", replay, err)
	}
	for _, changed := range []domain.Request{
		request("success", 101),
		{IdempotencyKey: "success", FromWalletID: "other", ToWalletID: "wallet_2", Amount: 100},
		{IdempotencyKey: "success", FromWalletID: "wallet_1", ToWalletID: "other", Amount: 100},
	} {
		if _, err := svc.Transfer(t.Context(), changed); !errors.Is(err, domain.ErrIdempotencyConflict) {
			t.Fatalf("changed payload: %v", err)
		}
	}
	if _, err := svc.Transfer(t.Context(), request("success", 0)); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("validation must precede replay: %v", err)
	}
	assertState(t, pool, 400, 300, 1, 0, 1)
}

func TestTransferBusinessFailures(t *testing.T) {
	for _, tc := range []struct {
		name, setup string
		req         domain.Request
		code        string
		httpStatus  int
		status      domain.Status
		toBalance   int64
	}{
		{"insufficient", "", request("failure", 501), "INSUFFICIENT_FUNDS", 422, domain.Failed, 200},
		{"overflow", "UPDATE wallets SET balance_minor=9223372036854775807 WHERE id='wallet_2'", request("failure", 1), "BALANCE_OVERFLOW", 422, domain.Failed, math.MaxInt64},
		{"missing source", "", domain.Request{IdempotencyKey: "failure", FromWalletID: "missing", ToWalletID: "wallet_2", Amount: 1}, "WALLET_NOT_FOUND", 404, "", 200},
		{"missing destination", "", domain.Request{IdempotencyKey: "failure", FromWalletID: "wallet_1", ToWalletID: "missing", Amount: 1}, "WALLET_NOT_FOUND", 404, "", 200},
		{"currency mismatch", "UPDATE wallets SET currency_code='USD' WHERE id='wallet_2'", request("failure", 1), "CURRENCY_MISMATCH", 422, "", 200},
		{"unsupported currency", "UPDATE wallets SET currency_code='XXX'", request("failure", 1), "UNSUPPORTED_CURRENCY", 422, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, _ := testutil.NewDatabase(t)
			if tc.setup != "" {
				execSQL(t, pool, tc.setup)
			}
			svc := New(pool)
			result, err := svc.Transfer(t.Context(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			assertResult(t, result, tc.httpStatus, tc.status, tc.code)
			failed := 0
			if tc.status == domain.Failed {
				failed = 1
			}
			assertState(t, pool, 500, tc.toBalance, 0, failed, 1)
			// Fix the cause; an old key must still replay its original rejection.
			execSQL(t, pool, `UPDATE wallets SET currency_code='INR', balance_minor=1000;
				INSERT INTO wallets(id,currency_code,balance_minor) VALUES ('missing','INR',1000)`)
			replay, err := svc.Transfer(t.Context(), tc.req)
			if err != nil || replay != result {
				t.Fatalf("failure replay: %+v %v", replay, err)
			}
			assertState(t, pool, 1000, 1000, 0, failed, 1)
			tc.req.IdempotencyKey = "new-attempt"
			next, err := svc.Transfer(t.Context(), tc.req)
			if err != nil {
				t.Fatal(err)
			}
			assertResult(t, next, 201, domain.Processed, "")
			from, to := int64(1000), int64(1000)
			if tc.req.FromWalletID == "wallet_1" {
				from -= tc.req.Amount
			}
			if tc.req.ToWalletID == "wallet_2" {
				to += tc.req.Amount
			}
			assertState(t, pool, from, to, 1, failed, 2)
		})
	}
}

func TestTransferInvalidInputIsNotSaved(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	svc := New(pool)
	for _, tc := range []struct {
		req  domain.Request
		want error
	}{
		{request("invalid", 0), domain.ErrInvalidRequest},
		{request("invalid", -1), domain.ErrInvalidRequest},
		{request("", 1), domain.ErrInvalidRequest},
		{domain.Request{IdempotencyKey: "invalid", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 1}, domain.ErrSelfTransfer},
	} {
		if _, err := svc.Transfer(t.Context(), tc.req); !errors.Is(err, tc.want) {
			t.Fatalf("got %v, want %v", err, tc.want)
		}
	}
	assertState(t, pool, 500, 200, 0, 0, 0)
}

type outcome struct {
	result repository.Result
	err    error
}

func waitForBlocked(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count >= n {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// Hold a wallet lock until every request is blocked in PostgreSQL, then release.
func concurrentTransfers(t *testing.T, pool *pgxpool.Pool, requests ...domain.Request) []outcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = blocker.Rollback(cleanup)
	}()
	if _, err := blocker.Exec(ctx, "SELECT id FROM wallets WHERE id='wallet_1' FOR UPDATE"); err != nil {
		t.Fatal(err)
	}
	results := make(chan outcome, len(requests))
	remaining := len(requests)
	defer func() {
		cancel()
		for remaining > 0 {
			<-results
			remaining--
		}
	}()
	svc := New(pool)
	for _, req := range requests {
		go func() { result, err := svc.Transfer(ctx, req); results <- outcome{result, err} }()
	}
	waitForBlocked(t, pool, len(requests))
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	out := make([]outcome, 0, len(requests))
	for remaining > 0 {
		out = append(out, <-results)
		remaining--
	}
	return out
}

func TestConcurrentDuplicates(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	req := request("same", 100)
	results := concurrentTransfers(t, pool, req, req, req)
	for _, out := range results {
		if out.err != nil || out.result != results[0].result {
			t.Fatalf("duplicate: %+v", out)
		}
		assertResult(t, out.result, 201, domain.Processed, "")
	}
	assertState(t, pool, 400, 300, 1, 0, 1)
}

func TestConcurrentDebits(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	execSQL(t, pool, "UPDATE wallets SET balance_minor=100 WHERE id='wallet_1'")
	results := concurrentTransfers(t, pool, request("one", 80), request("two", 80))
	success, failed := 0, 0
	for _, out := range results {
		if out.err != nil {
			t.Fatal(out.err)
		}
		if out.result.HTTPStatus == 201 {
			success++
			assertResult(t, out.result, 201, domain.Processed, "")
		} else {
			failed++
			assertResult(t, out.result, 422, domain.Failed, "INSUFFICIENT_FUNDS")
		}
	}
	if success != 1 || failed != 1 {
		t.Fatalf("success=%d failed=%d", success, failed)
	}
	assertState(t, pool, 20, 280, 1, 1, 2)
}

func TestConcurrentDifferentPayloads(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	results := concurrentTransfers(t, pool, request("same", 80), request("same", 100))
	var successes, conflicts int
	for _, out := range results {
		if errors.Is(out.err, domain.ErrIdempotencyConflict) {
			conflicts++
			continue
		}
		if out.err != nil {
			t.Fatal(out.err)
		}
		assertResult(t, out.result, 201, domain.Processed, "")
		successes++
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	var amount int64
	if err := pool.QueryRow(t.Context(), "SELECT amount_minor FROM transfers").Scan(&amount); err != nil {
		t.Fatal(err)
	}
	assertState(t, pool, 500-amount, 200+amount, 1, 0, 1)
}

func TestTransferRollbackAndRetry(t *testing.T) {
	for _, tc := range []struct{ name, table, when, condition string }{
		{"after balance write", "wallets", "AFTER UPDATE", "NEW.id='wallet_2'"},
		{"after ledger insert", "ledger_entries", "AFTER INSERT", "NEW.entry_type='CREDIT'"},
		{"before saving response", "idempotency_records", "BEFORE UPDATE", "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool, _ := testutil.NewDatabase(t)
			execSQL(t, pool, fmt.Sprintf(`CREATE FUNCTION inject_failure() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN RAISE EXCEPTION 'injected failure' USING ERRCODE='23514'; END $$;
				CREATE TRIGGER inject_failure %s ON %s FOR EACH ROW WHEN (%s) EXECUTE FUNCTION inject_failure()`, tc.when, tc.table, tc.condition))
			svc := New(pool)
			result, err := svc.Transfer(t.Context(), request("retry", 100))
			if err == nil || errors.Is(err, ErrUnavailable) || result != (repository.Result{}) {
				t.Fatalf("failure: %+v, %v", result, err)
			}
			assertState(t, pool, 500, 200, 0, 0, 0)
			execSQL(t, pool, "DROP TRIGGER inject_failure ON "+tc.table)
			result, err = svc.Transfer(t.Context(), request("retry", 100))
			if err != nil {
				t.Fatal(err)
			}
			assertResult(t, result, 201, domain.Processed, "")
			assertState(t, pool, 400, 300, 1, 0, 1)
		})
	}
}

func TestTransferCancellationAndRetry(t *testing.T) {
	for _, waitOn := range []string{"wallet", "key"} {
		t.Run(waitOn, func(t *testing.T) {
			pool, _ := testutil.NewDatabase(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			blocker, err := pool.Begin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				cleanup, stop := context.WithTimeout(context.Background(), time.Second)
				defer stop()
				_ = blocker.Rollback(cleanup)
			}()
			if waitOn == "wallet" {
				_, err = blocker.Exec(ctx, "SELECT id FROM wallets WHERE id='wallet_1' FOR UPDATE")
			} else {
				_, err = repository.New(blocker).ClaimOrLoad(ctx, "cancel", domain.Payload{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100})
			}
			if err != nil {
				t.Fatal(err)
			}
			svc := New(pool)
			done := make(chan outcome, 1)
			defer func() {
				cancel()
				for range done {
				}
			}()
			go func() {
				defer close(done)
				result, err := svc.Transfer(ctx, request("cancel", 100))
				done <- outcome{result, err}
			}()
			waitForBlocked(t, pool, 1)
			cancel()
			out := <-done
			if !errors.Is(out.err, ErrUnavailable) || !errors.Is(out.err, context.Canceled) || out.result != (repository.Result{}) {
				t.Fatalf("cancellation: %+v", out)
			}
			if err := blocker.Rollback(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertState(t, pool, 500, 200, 0, 0, 0)
			result, err := svc.Transfer(t.Context(), request("cancel", 100))
			if err != nil {
				t.Fatal(err)
			}
			assertResult(t, result, 201, domain.Processed, "")
			assertState(t, pool, 400, 300, 1, 0, 1)
		})
	}
}

func TestTransferDeadlineAndUnavailablePool(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	svc := New(pool)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := svc.Transfer(ctx, request("deadline", 100)); !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	assertState(t, pool, 500, 200, 0, 0, 0)
	pool.Close()
	if _, err := svc.Transfer(t.Context(), request("closed", 100)); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed pool: %v", err)
	}
}

func TestTransferCommitRejection(t *testing.T) {
	for _, code := range []string{"23514", "40001", "40P01", "57014"} {
		t.Run(code, func(t *testing.T) {
			pool, _ := testutil.NewDatabase(t)
			execSQL(t, pool, fmt.Sprintf(`CREATE FUNCTION reject_commit() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN RAISE EXCEPTION 'commit rejected' USING ERRCODE='%s'; END $$;
				CREATE CONSTRAINT TRIGGER reject_commit AFTER INSERT ON transfers
				DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_commit()`, code))
			svc := New(pool)
			var logs bytes.Buffer
			oldLogger := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(oldLogger) })
			result, err := svc.Transfer(t.Context(), request("commit", 100))
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != code || errors.Is(err, ErrCommitUnknown) || errors.Is(err, ErrUnavailable) != (code != "23514") || result != (repository.Result{}) {
				t.Fatalf("commit rejection: %+v, %v", result, err)
			}
			var entry struct {
				Outcome  string `json:"outcome"`
				SQLState string `json:"sqlstate"`
			}
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil || entry.Outcome != "rolled_back" || entry.SQLState != code {
				t.Fatalf("commit rejection log: %s (%v)", logs.Bytes(), err)
			}
			assertState(t, pool, 500, 200, 0, 0, 0)
			execSQL(t, pool, "DROP TRIGGER reject_commit ON transfers")
			if _, err := svc.Transfer(t.Context(), request("commit", 100)); err != nil {
				t.Fatal(err)
			}
			assertState(t, pool, 400, 300, 1, 0, 1)
		})
	}
}

// Simulate a lost acknowledgment after a real PostgreSQL commit.
type lostAckTx struct{ pgx.Tx }

func (tx lostAckTx) Commit(ctx context.Context) error {
	if err := tx.Tx.Commit(ctx); err != nil {
		return err
	}
	return io.EOF
}

func TestUnconfirmedCommitCanBeReplayed(t *testing.T) {
	pool, _ := testutil.NewDatabase(t)
	ctx := t.Context()
	tx, err := database.BeginReadCommitted(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
	}()
	repo := repository.New(tx)
	payload := domain.Payload{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	if _, err := repo.ClaimOrLoad(ctx, "lost-ack", payload); err != nil {
		t.Fatal(err)
	}
	from, to, err := repo.LockWallets(ctx, "wallet_1", "wallet_2")
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := repo.CreatePending(ctx, payload, "INR")
	if err != nil {
		t.Fatal(err)
	}
	from.Balance -= 100
	to.Balance += 100
	if err := repo.UpdateBalances(ctx, from, to); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertLedgerPair(ctx, transfer.ID); err != nil {
		t.Fatal(err)
	}
	transfer.Status = domain.Processed
	if err := repo.CompleteTransfer(ctx, transfer.ID, transfer.Status, ""); err != nil {
		t.Fatal(err)
	}
	result, err := saveAndCommit(ctx, lostAckTx{tx}, "lost-ack", transfer, "")
	err = classifyError(err)
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrCommitUnknown) || !errors.Is(err, io.EOF) || result != (repository.Result{}) {
		t.Fatalf("unknown commit: %+v %v", result, err)
	}
	var logs bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	logTransfer(WithRequestID(ctx, "lost-ack-request"), time.Now(), "", transfer.ID, result, err)
	var entry struct {
		Outcome   string `json:"outcome"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil || entry.Outcome != "unknown" || entry.RequestID != "lost-ack-request" {
		t.Fatalf("unconfirmed commit log: %s (%v)", logs.Bytes(), err)
	}
	assertState(t, pool, 400, 300, 1, 0, 1)
	replay, err := New(pool).Transfer(ctx, request("lost-ack", 100))
	if err != nil || replay.TransferID != transfer.ID {
		t.Fatalf("retry: %+v %v", replay, err)
	}
	assertResult(t, replay, 201, domain.Processed, "")
	assertState(t, pool, 400, 300, 1, 0, 1)
}
