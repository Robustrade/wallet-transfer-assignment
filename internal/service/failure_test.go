package service_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/service"
)

// This test-only pool performs a real COMMIT, then loses its acknowledgement.
// It exercises the uncertain outcome without adding production fault hooks.
type lostAckPool struct {
	*sql.DB
	armed atomic.Bool
}

func (p *lostAckPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &lostAckTransaction{Tx: tx, pool: p}, nil
}

type lostAckTransaction struct {
	*sql.Tx
	pool *lostAckPool
}

func (tx *lostAckTransaction) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.pool.armed.CompareAndSwap(true, false) {
		return io.EOF
	}
	return nil
}

func (f fixture) loseNextCommitAck() fixture {
	f.t.Helper()
	sqlDB, err := f.db.DB()
	if err != nil {
		f.t.Fatal(err)
	}
	pool := &lostAckPool{DB: sqlDB}
	pool.armed.Store(true)
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		f.t.Fatal(err)
	}
	f.svc = service.NewTransferService(db, 5*time.Second, testMaxTransferAmount)
	return f
}

func TestFailure_LostReservationCommitAckResumesSameTransfer(t *testing.T) {
	f := setup(t).loseNextCommitAck()
	first := f.post(transferRequest("k1", 100))
	if first.Code != 503 {
		t.Fatalf("unknown reservation response: %d %s", first.Code, first.Body)
	}
	var id string
	if err := f.db.WithContext(f.ctx).Raw("SELECT id FROM transfers").Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.wantPending(id)
	f.wantBalance("wallet_1", 1000)
	f.wantCount("ledger_entries", 0)
	second := f.post(transferRequest("k1", 100))
	if second.Code != 201 {
		t.Fatalf("reservation retry: %d %s", second.Code, second.Body)
	}
	f.wantCount("transfers", 1)
	f.wantBalance("wallet_1", 900)
	f.wantReconciled()
}

func TestFailure_LostProcessingCommitAckReplaysCommittedResult(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	f = f.loseNextCommitAck()
	first := f.post(transferRequest("k1", 100))
	if first.Code != 503 {
		t.Fatalf("unknown processing response: %d %s", first.Code, first.Body)
	}
	f.wantBalance("wallet_1", 900)
	second := f.post(transferRequest("k1", 100))
	if second.Code != 201 || second.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("committed transfer did not replay after uncertain outcome")
	}
	f.wantState(id, "PROCESSED")
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
	f.wantReconciled()
}

func TestFailure_UnavailableDatabaseReturnsGeneric503(t *testing.T) {
	f := setup(t)
	listener, err := (&net.ListenConfig{}).Listen(f.ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	u, err := url.Parse(f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = listener.Addr().String()
	db, err := gorm.Open(postgres.Open(u.String()), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	f.svc = service.NewTransferService(db, 100*time.Millisecond, testMaxTransferAmount)
	response := f.post(transferRequest("k1", 100))
	if response.Code != 503 || strings.Contains(response.Body.String(), "127.0.0.1") || strings.Contains(response.Body.String(), "postgres://") {
		t.Fatalf("unavailable database response: %d %s", response.Code, response.Body)
	}
	f.wantCount("transfers", 0)
	f.wantCount("idempotency_records", 0)
}

func TestFailure_TerminatedBackendRollsBackDebitAndCanRetry(t *testing.T) {
	f := setup(t)
	blocker := f.db.WithContext(f.ctx).Begin()
	if blocker.Error != nil {
		t.Fatal(blocker.Error)
	}
	defer func() { _ = blocker.Rollback().Error }()
	if err := blocker.Exec("SELECT pg_advisory_xact_lock(90500001)").Error; err != nil {
		t.Fatal(err)
	}
	var blockerPID int
	if err := blocker.Raw("SELECT pg_backend_pid()").Row().Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	f.exec(`CREATE FUNCTION block_credit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id='wallet_2' AND NEW.balance>OLD.balance THEN PERFORM pg_advisory_xact_lock(90500001); END IF;
    RETURN NEW;
END; $$;
CREATE TRIGGER block_credit BEFORE UPDATE ON wallets FOR EACH ROW EXECUTE FUNCTION block_credit();`)
	response := httptest.NewRecorder()
	request := f.httpRequest(transferRequest("k1", 100))
	done := make(chan struct{})
	go func() {
		defer close(done)
		handler.NewRouter(f.svc, testMaxTransferAmount).ServeHTTP(response, request)
	}()
	pid := f.waitForBlockedBackend(blockerPID)
	var terminated bool
	if err := f.db.WithContext(f.ctx).Raw("SELECT pg_terminate_backend(?)", pid).Row().Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate backend: %v", err)
	}
	select {
	case <-done:
	case <-f.ctx.Done():
		t.Fatal("request did not finish after backend termination")
	}
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("terminated backend response: %d %s", response.Code, response.Body)
	}
	var id string
	if err := f.db.WithContext(f.ctx).Raw("SELECT id FROM transfers").Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.wantPending(id)
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
	f.wantCount("ledger_entries", 0)
	if err := blocker.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	f.exec("DROP TRIGGER block_credit ON wallets")
	if response := f.post(transferRequest("k1", 100)); response.Code != 201 {
		t.Fatalf("backend failure retry: %d %s", response.Code, response.Body)
	}
	f.wantCount("transfers", 1)
	f.wantBalance("wallet_1", 900)
	f.wantReconciled()
}

func (f fixture) waitForBlockedBackend(blockerPID int) int {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pid int
		err := f.db.WithContext(ctx).Raw(`SELECT pid FROM pg_stat_activity
			WHERE application_name=? AND ?=ANY(pg_blocking_pids(pid))`, f.application, blockerPID).Row().Scan(&pid)
		if err == nil {
			return pid
		}
		if !errors.Is(err, sql.ErrNoRows) {
			f.t.Fatalf("observe blocked backend: %v", err)
		}
		select {
		case <-ctx.Done():
			f.t.Fatal("processing backend did not reach blocked credit")
		case <-ticker.C:
		}
	}
}
