package repository

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
)

func newTestRepository(t *testing.T) *PostgresRepository {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to execute PostgreSQL repository integration tests")
	}
	repository, err := OpenPostgres(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository
}

func orderedWalletIDs() (string, string) {
	ids := []string{uuid.NewString(), uuid.NewString()}
	sort.Strings(ids)
	return ids[0], ids[1]
}

// TestRepositoryTransferLifecycle verifies schema migration, balances, ledger creation, and history queries.
func TestRepositoryTransferLifecycle(t *testing.T) {
	repository := newTestRepository(t)
	from, to := uuid.NewString(), uuid.NewString()
	if err := repository.CreateWallet(context.Background(), from, 100); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateWallet(context.Background(), to, 0); err != nil {
		t.Fatal(err)
	}
	transfer, err := repository.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 40})
	if err != nil {
		t.Fatal(err)
	}
	if entries, err := repository.LedgerEntryCount(context.Background(), transfer.ID); err != nil || entries != 2 {
		t.Fatalf("entries=%d err=%v", entries, err)
	}
	if balance, err := repository.Balance(context.Background(), from); err != nil || balance != 60 {
		t.Fatalf("balance=%d err=%v", balance, err)
	}
	history, err := repository.TransferHistory(context.Background(), from, 10)
	if err != nil || len(history) != 1 || history[0].ID != transfer.ID {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

// TestRepositoryRejectsBadWalletAndUnknownHistory verifies repository query error handling.
func TestRepositoryRejectsBadWalletAndUnknownHistory(t *testing.T) {
	repository := newTestRepository(t)
	if err := repository.CreateWallet(context.Background(), "not-a-uuid", 0); err == nil {
		t.Fatal("expected invalid UUID error")
	}
	if _, err := repository.TransferHistory(context.Background(), uuid.NewString(), 10); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("err=%v", err)
	}
}

// TestRetryableTransactionErrorRecognizesOnlyRetrySafePostgresCodes verifies retry classification.
func TestRetryableTransactionErrorRecognizesOnlyRetrySafePostgresCodes(t *testing.T) {
	if !isRetryableTransaction(&pgconn.PgError{Code: "40001"}) || !isRetryableTransaction(&pgconn.PgError{Code: "40P01"}) {
		t.Fatal("expected serialization and deadlock errors to be retryable")
	}
	if isRetryableTransaction(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("unique violations must not be retried")
	}
}

// TestRetryTransactionRetriesConflictsAndHonorsCancellation verifies bounded retry control flow.
func TestRetryTransactionRetriesConflictsAndHonorsCancellation(t *testing.T) {
	calls := 0
	expected := domain.Transfer{ID: uuid.NewString()}
	transfer, err := retryTransaction(context.Background(), func(context.Context) (domain.Transfer, error) {
		calls++
		if calls == 1 {
			return domain.Transfer{}, &pgconn.PgError{Code: "40001"}
		}
		return expected, nil
	})
	if err != nil || calls != 2 || transfer.ID != expected.ID {
		t.Fatalf("transfer=%#v calls=%d err=%v", transfer, calls, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := retryTransaction(canceled, func(context.Context) (domain.Transfer, error) {
		return domain.Transfer{}, &pgconn.PgError{Code: "40P01"}
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	calls = 0
	if _, err := retryTransaction(context.Background(), func(context.Context) (domain.Transfer, error) {
		calls++
		return domain.Transfer{}, &pgconn.PgError{Code: "40001"}
	}); err == nil || calls != maxTransactionAttempts {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	nonRetryable := errors.New("validation failure")
	if _, err := retryTransaction(context.Background(), func(context.Context) (domain.Transfer, error) {
		return domain.Transfer{}, nonRetryable
	}); !errors.Is(err, nonRetryable) {
		t.Fatalf("err=%v", err)
	}
}

// TestRepositoryReplaysConflictsAndRecordsFailedTransfers verifies idempotency terminal outcomes.
func TestRepositoryReplaysConflictsAndRecordsFailedTransfers(t *testing.T) {
	repository := newTestRepository(t)
	from, to := uuid.NewString(), uuid.NewString()
	for id, balance := range map[string]int64{from: 10, to: 0} {
		if err := repository.CreateWallet(context.Background(), id, balance); err != nil {
			t.Fatal(err)
		}
	}
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 5}
	first, err := repository.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.CreateTransfer(context.Background(), request)
	if err != nil || !replayed.Replayed || replayed.ID != first.ID {
		t.Fatalf("transfer=%#v err=%v", replayed, err)
	}
	conflict := request
	conflict.Amount = 4
	if _, err := repository.CreateTransfer(context.Background(), conflict); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err=%v", err)
	}
	failedRequest := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 100}
	failed, err := repository.CreateTransfer(context.Background(), failedRequest)
	if !errors.Is(err, domain.ErrInsufficientFunds) || failed.Status != domain.Failed {
		t.Fatalf("transfer=%#v err=%v", failed, err)
	}
	failedReplay, err := repository.CreateTransfer(context.Background(), failedRequest)
	if !errors.Is(err, domain.ErrInsufficientFunds) || !failedReplay.Replayed {
		t.Fatalf("transfer=%#v err=%v", failedReplay, err)
	}
}

// TestRepositoryHelpersCoverDuplicateAndMissingWalletBranches verifies repository-only branches.
func TestRepositoryHelpersCoverDuplicateAndMissingWalletBranches(t *testing.T) {
	repository := newTestRepository(t)
	from, to := uuid.NewString(), uuid.NewString()
	if err := repository.CreateWallet(context.Background(), from, 10); err != nil {
		t.Fatal(err)
	}
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 1}
	if _, err := repository.CreateTransfer(context.Background(), request); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("err=%v", err)
	}
	missingSource := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: uuid.NewString(), ToWalletID: from, Amount: 1}
	if _, err := repository.CreateTransfer(context.Background(), missingSource); !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("err=%v", err)
	}
	if _, err := repository.replayAfterDuplicate(context.Background(), uuid.NewString(), "fingerprint"); err == nil {
		t.Fatal("expected missing duplicate record error")
	}
	if !isUniqueViolation(&pgconn.PgError{Code: "23505"}) || isUniqueViolation(&pgconn.PgError{Code: "40001"}) {
		t.Fatal("unexpected unique violation classification")
	}
}

// TestRepositoryClosedConnectionErrors verifies database-operation failures are returned rather than hidden.
func TestRepositoryClosedConnectionErrors(t *testing.T) {
	repository := newTestRepository(t)
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateWallet(context.Background(), uuid.NewString(), 0); err == nil {
		t.Fatal("expected closed database error")
	}
	if _, err := repository.Balance(context.Background(), uuid.NewString()); err == nil {
		t.Fatal("expected closed database error")
	}
	if err := repository.CreateWallet(context.Background(), uuid.NewString(), -1); err == nil {
		t.Fatal("expected negative balance validation error")
	}
}

// TestOpenPostgresReturnsCanceledConnectionErrors verifies connection startup failures are surfaced.
func TestOpenPostgresReturnsCanceledConnectionErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenPostgres(ctx, "postgres://wallet:wallet@localhost:5433/wallets?sslmode=disable"); err == nil {
		t.Fatal("expected canceled connection error")
	}
}

// TestMigrationRunnerExecutesUnrecordedFiles verifies embedded migrations are recorded atomically.
func TestMigrationRunnerExecutesUnrecordedFiles(t *testing.T) {
	repository := newTestRepository(t)
	if _, err := repository.db.ExecContext(context.Background(), "DELETE FROM schema_migrations"); err != nil {
		t.Fatal(err)
	}
	if err := repository.migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := repository.db.QueryRowContext(context.Background(), "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count == 0 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

// TestMigrationRunnerReturnsDatabaseSetupErrors verifies migration setup failures are surfaced.
func TestMigrationRunnerReturnsDatabaseSetupErrors(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectClose()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := (&PostgresRepository{db: db}).migrate(context.Background()); err == nil {
		t.Fatal("expected migration setup error")
	}
}

// TestMigrationRunnerReturnsDriverErrors verifies lookup and transaction-start failures are surfaced.
func TestMigrationRunnerReturnsDriverErrors(t *testing.T) {
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
	}{
		{"lookup", func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT EXISTS").WithArgs("001_initial").WillReturnError(errors.New("lookup failed"))
		}},
		{"begin", func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT EXISTS").WithArgs("001_initial").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			mock.ExpectBegin().WillReturnError(errors.New("begin failed"))
		}},
		{"statement", func(mock sqlmock.Sqlmock) {
			mock.ExpectExec("CREATE TABLE IF NOT EXISTS schema_migrations").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT EXISTS").WithArgs("001_initial").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
			mock.ExpectBegin()
			mock.ExpectExec("DO").WillReturnError(errors.New("statement failed"))
			mock.ExpectRollback()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			test.setup(mock)
			if err := (&PostgresRepository{db: db}).migrate(context.Background()); err == nil {
				t.Fatal("expected error")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestReplayAfterDuplicateReturnsExistingOutcome verifies the unique-key race recovery helper.
func TestReplayAfterDuplicateReturnsExistingOutcome(t *testing.T) {
	repository := newTestRepository(t)
	from, to := uuid.NewString(), uuid.NewString()
	for id, balance := range map[string]int64{from: 10, to: 0} {
		if err := repository.CreateWallet(context.Background(), id, balance); err != nil {
			t.Fatal(err)
		}
	}
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 1}
	created, err := repository.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := repository.replayAfterDuplicate(context.Background(), request.IdempotencyKey, requestFingerprint(request))
	if err != nil || !replayed.Replayed || replayed.ID != created.ID {
		t.Fatalf("transfer=%#v err=%v", replayed, err)
	}
	if _, err := repository.replayAfterDuplicate(context.Background(), request.IdempotencyKey, "different"); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err=%v", err)
	}
	failedRequest := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 100}
	if _, err := repository.CreateTransfer(context.Background(), failedRequest); !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatal(err)
	}
	if _, err := repository.replayAfterDuplicate(context.Background(), failedRequest.IdempotencyKey, requestFingerprint(failedRequest)); !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("err=%v", err)
	}
}

// TestRepositoryConcurrentDuplicateRequestsUseUniqueKeyRecovery verifies duplicate-request race handling.
func TestRepositoryConcurrentDuplicateRequestsUseUniqueKeyRecovery(t *testing.T) {
	repository := newTestRepository(t)
	from, to := uuid.NewString(), uuid.NewString()
	for id, balance := range map[string]int64{from: 10, to: 0} {
		if err := repository.CreateWallet(context.Background(), id, balance); err != nil {
			t.Fatal(err)
		}
	}
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 1}
	start := make(chan struct{})
	results := make(chan domain.Transfer, 2)
	errors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			transfer, err := repository.CreateTransfer(context.Background(), request)
			results <- transfer
			errors <- err
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(errors)
	transfers := make([]domain.Transfer, 0, 2)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for transfer := range results {
		transfers = append(transfers, transfer)
	}
	if len(transfers) != 2 || transfers[0].ID != transfers[1].ID || !(transfers[0].Replayed || transfers[1].Replayed) {
		t.Fatalf("transfers=%#v", transfers)
	}
}

// TestCreateTransferOnceRollsBackOnRepositoryFailures verifies transactional error propagation and rollback.
func TestCreateTransferOnceRollsBackOnRepositoryFailures(t *testing.T) {
	from, to := orderedWalletIDs()
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 1}
	t.Run("begin failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin().WillReturnError(errors.New("begin failed"))
		if _, err := (&PostgresRepository{db: db}).createTransferOnce(context.Background(), request); err == nil {
			t.Fatal("expected error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("idempotency lookup failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT t.id").WithArgs(request.IdempotencyKey).WillReturnError(errors.New("query failed"))
		mock.ExpectRollback()
		if _, err := (&PostgresRepository{db: db}).createTransferOnce(context.Background(), request); err == nil {
			t.Fatal("expected error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ledger insert failure", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT t.id").WithArgs(request.IdempotencyKey).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		mock.ExpectExec("INSERT INTO transfers").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO idempotency_records").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(from).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10))
		mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(to).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(0))
		mock.ExpectExec("UPDATE wallets SET balance=balance-").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("UPDATE wallets SET balance=balance\\+").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectExec("INSERT INTO ledger_entries").WillReturnError(errors.New("ledger failed"))
		mock.ExpectRollback()
		if _, err := (&PostgresRepository{db: db}).createTransferOnce(context.Background(), request); err == nil {
			t.Fatal("expected error")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}

// TestRepositoryQueryAndInsertErrorsPropagate verifies non-transactional database failures are not hidden.
func TestRepositoryQueryAndInsertErrorsPropagate(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := &PostgresRepository{db: db}
	mock.ExpectExec("INSERT INTO wallets").WillReturnError(errors.New("insert failed"))
	if err := repository.CreateWallet(context.Background(), uuid.NewString(), 0); err == nil {
		t.Fatal("expected insert error")
	}
	walletID := uuid.NewString()
	mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1").WithArgs(walletID).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(0))
	mock.ExpectQuery("SELECT id,idempotency_key").WithArgs(walletID, 10).WillReturnError(errors.New("history failed"))
	if _, err := repository.TransferHistory(context.Background(), walletID, 10); err == nil {
		t.Fatal("expected history error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestCreateTransferOncePropagatesWorkflowWriteFailures verifies every critical write aborts its transaction.
func TestCreateTransferOncePropagatesWorkflowWriteFailures(t *testing.T) {
	from, to := orderedWalletIDs()
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 1}
	prepare := func(t *testing.T) (sqlmock.Sqlmock, *PostgresRepository, func()) {
		t.Helper()
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT t.id").WithArgs(request.IdempotencyKey).WillReturnRows(sqlmock.NewRows([]string{"id"}))
		return mock, &PostgresRepository{db: db}, func() { _ = db.Close() }
	}
	expectTransfer := func(mock sqlmock.Sqlmock) {
		mock.ExpectExec("INSERT INTO transfers").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	expectRecords := func(mock sqlmock.Sqlmock) {
		mock.ExpectExec("INSERT INTO idempotency_records").WillReturnResult(sqlmock.NewResult(1, 1))
	}
	expectLocks := func(mock sqlmock.Sqlmock, fromBalance int64) {
		mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(from).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(fromBalance))
		mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(to).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(0))
	}
	tests := []struct {
		name  string
		setup func(sqlmock.Sqlmock)
	}{
		{"idempotency record", func(mock sqlmock.Sqlmock) {
			expectTransfer(mock)
			mock.ExpectExec("INSERT INTO idempotency_records").WillReturnError(errors.New("idempotency failed"))
		}},
		{"wallet lock", func(mock sqlmock.Sqlmock) {
			expectTransfer(mock)
			expectRecords(mock)
			mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(from).WillReturnError(errors.New("lock failed"))
		}},
		{"failed status", func(mock sqlmock.Sqlmock) {
			expectTransfer(mock)
			expectRecords(mock)
			expectLocks(mock, 0)
			mock.ExpectExec("UPDATE transfers SET status=\\$1, failure_reason=\\$2").WillReturnError(errors.New("status failed"))
		}},
		{"debit balance", func(mock sqlmock.Sqlmock) {
			expectTransfer(mock)
			expectRecords(mock)
			expectLocks(mock, 10)
			mock.ExpectExec("UPDATE wallets SET balance=balance-").WillReturnError(errors.New("debit failed"))
		}},
		{"processed status", func(mock sqlmock.Sqlmock) {
			expectTransfer(mock)
			expectRecords(mock)
			expectLocks(mock, 10)
			mock.ExpectExec("UPDATE wallets SET balance=balance-").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE wallets SET balance=balance\\+").WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("INSERT INTO ledger_entries").WillReturnResult(sqlmock.NewResult(1, 2))
			mock.ExpectExec("UPDATE transfers SET status=\\$1 WHERE id=\\$2").WillReturnError(errors.New("processed status failed"))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mock, repository, closeDB := prepare(t)
			defer closeDB()
			test.setup(mock)
			mock.ExpectRollback()
			if _, err := repository.createTransferOnce(context.Background(), request); err == nil {
				t.Fatal("expected error")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestCreateTransferOnceReturnsCommitFailures verifies results are not reported before commit succeeds.
func TestCreateTransferOnceReturnsCommitFailures(t *testing.T) {
	from, to := orderedWalletIDs()
	request := domain.CreateTransferRequest{IdempotencyKey: uuid.NewString(), FromWalletID: from, ToWalletID: to, Amount: 1}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT t.id").WithArgs(request.IdempotencyKey).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectExec("INSERT INTO transfers").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO idempotency_records").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(from).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10))
	mock.ExpectQuery("SELECT balance FROM wallets WHERE id=\\$1 FOR UPDATE").WithArgs(to).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(0))
	mock.ExpectExec("UPDATE wallets SET balance=balance-").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE wallets SET balance=balance\\+").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO ledger_entries").WillReturnResult(sqlmock.NewResult(1, 2))
	mock.ExpectExec("UPDATE transfers SET status=\\$1 WHERE id=\\$2").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
	if _, err := (&PostgresRepository{db: db}).createTransferOnce(context.Background(), request); err == nil {
		t.Fatal("expected commit error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
