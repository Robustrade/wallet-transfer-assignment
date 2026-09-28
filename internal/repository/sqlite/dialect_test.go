package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDialect_Settings(t *testing.T) {
	t.Parallel()
	d := Dialect{}

	assert.Equal(t, "sqlite", d.Name())
	assert.Equal(t, "SELECT * FROM t WHERE a = ? AND b = ?", d.Rebind("SELECT * FROM t WHERE a = ? AND b = ?"))
	assert.Equal(t, "", d.LockClause(), "SQLite has no row locks; BEGIN IMMEDIATE already holds the write lock")
	assert.Nil(t, d.TxOptions())
	assert.NoError(t, d.SetupTx(context.Background(), nil))
}

// SQLite errors cannot be built by hand (their fields are unexported), so
// these tests provoke real ones.
func TestDialect_ErrorClassification(t *testing.T) {
	t.Parallel()
	d := Dialect{}
	ctx := context.Background()

	newDSN := func(t *testing.T) string {
		t.Helper()
		return "file:" + filepath.Join(t.TempDir(), "d.db")
	}

	t.Run("unique violation", func(t *testing.T) {
		t.Parallel()
		db, err := Open(newDSN(t), time.Second)
		require.NoError(t, err)
		defer db.Close()
		_, err = db.Exec("CREATE TABLE t (k TEXT PRIMARY KEY, v TEXT UNIQUE)")
		require.NoError(t, err)
		_, err = db.Exec("INSERT INTO t VALUES ('a', 'x')")
		require.NoError(t, err)

		_, pkErr := db.Exec("INSERT INTO t VALUES ('a', 'y')")
		_, uqErr := db.Exec("INSERT INTO t VALUES ('b', 'x')")

		for _, err := range []error{pkErr, uqErr, fmt.Errorf("wrapped: %w", pkErr)} {
			require.Error(t, err)
			assert.True(t, d.IsUniqueViolation(err), "%v", err)
			assert.False(t, d.IsRetryable(err))
			assert.False(t, d.IsLockTimeout(err))
		}
	})

	t.Run("check violation is not a unique violation", func(t *testing.T) {
		t.Parallel()
		db, err := Open(newDSN(t), time.Second)
		require.NoError(t, err)
		defer db.Close()
		_, err = db.Exec("CREATE TABLE t (n INTEGER CHECK (n > 0))")
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO t VALUES (0)")
		require.Error(t, err)
		assert.False(t, d.IsUniqueViolation(err))
	})

	t.Run("SQLITE_BUSY after busy_timeout is a lock timeout, not retryable", func(t *testing.T) {
		t.Parallel()
		db, err := Open(newDSN(t), 100*time.Millisecond)
		require.NoError(t, err)
		defer db.Close()
		holder, err := db.Conn(ctx)
		require.NoError(t, err)
		defer holder.Close()
		_, err = holder.ExecContext(ctx, "BEGIN IMMEDIATE")
		require.NoError(t, err)
		defer func() { _, _ = holder.ExecContext(ctx, "ROLLBACK") }()

		start := time.Now()
		_, err = db.BeginTx(ctx, nil) // _txlock=immediate: needs the write lock too
		require.Error(t, err)
		assert.GreaterOrEqual(t, time.Since(start), 80*time.Millisecond, "busy_timeout should make it wait")
		assert.True(t, d.IsLockTimeout(err), "%v", err)
		assert.False(t, d.IsRetryable(err), "%v", err)
		assert.False(t, d.IsUniqueViolation(err))
	})

	t.Run("SQLITE_BUSY_SNAPSHOT is retryable", func(t *testing.T) {
		t.Parallel()
		// In WAL mode, a read transaction that later tries to write fails
		// immediately with BUSY_SNAPSHOT if another connection committed
		// in between. Waiting cannot help, but restarting the whole
		// transaction can. Our write transactions use BEGIN IMMEDIATE and
		// should never see this; it is classified defensively.
		db, err := Open(newDSN(t), time.Second)
		require.NoError(t, err)
		defer db.Close()
		_, err = db.Exec("CREATE TABLE t (n INTEGER)")
		require.NoError(t, err)

		reader, err := db.Conn(ctx)
		require.NoError(t, err)
		defer reader.Close()
		_, err = reader.ExecContext(ctx, "BEGIN DEFERRED")
		require.NoError(t, err)
		defer func() { _, _ = reader.ExecContext(ctx, "ROLLBACK") }()
		var n int
		require.NoError(t, reader.QueryRowContext(ctx, "SELECT COUNT(*) FROM t").Scan(&n)) // takes the read snapshot

		_, err = db.Exec("INSERT INTO t VALUES (1)") // another connection commits
		require.NoError(t, err)

		_, err = reader.ExecContext(ctx, "INSERT INTO t VALUES (2)") // stale snapshot tries to write
		require.Error(t, err)
		assert.True(t, d.IsRetryable(err), "%v", err)
		assert.False(t, d.IsLockTimeout(err), "%v", err)
	})

	t.Run("non-sqlite errors are not classified", func(t *testing.T) {
		t.Parallel()
		for _, err := range []error{nil, errors.New("boom")} {
			assert.False(t, d.IsRetryable(err))
			assert.False(t, d.IsLockTimeout(err))
			assert.False(t, d.IsUniqueViolation(err))
		}
	})
}

func TestOpen_ConfiguresEveryConnection(t *testing.T) {
	t.Parallel()
	db, err := Open("file:"+filepath.Join(t.TempDir(), "p.db"), 1500*time.Millisecond)
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxIdleConns(0) // force new connections so we check more than one

	for i := 0; i < 3; i++ {
		var fk, busy int
		var mode string
		require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))
		require.NoError(t, db.QueryRow("PRAGMA busy_timeout").Scan(&busy))
		require.NoError(t, db.QueryRow("PRAGMA journal_mode").Scan(&mode))
		assert.Equal(t, 1, fk, "foreign_keys")
		assert.Equal(t, 1500, busy, "busy_timeout in ms")
		assert.Equal(t, "wal", mode)
	}
}

func TestOpen_AcceptsDSNWithExistingQuery(t *testing.T) {
	t.Parallel()
	db, err := Open("file:"+filepath.Join(t.TempDir(), "q.db")+"?cache=private", time.Second)
	require.NoError(t, err)
	defer db.Close()
	var fk int
	require.NoError(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	assert.Equal(t, 1, fk)
}
