package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	sqlite "modernc.org/sqlite"
)

type Driver string

const (
	DriverPostgres Driver = "postgres"
	DriverSQLite   Driver = "sqlite"
)

type DB struct {
	*sql.DB
	driver Driver
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	driver, name, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}

	handle, err := sql.Open(driverName(driver), name)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", driver, err)
	}

	if driver == DriverSQLite {
		handle.SetMaxOpenConns(8)
	} else {
		handle.SetMaxOpenConns(20)
	}
	handle.SetMaxIdleConns(4)

	if err := handle.PingContext(ctx); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("ping %s database: %w", driver, err)
	}
	return &DB{DB: handle, driver: driver}, nil
}

func (db *DB) Driver() Driver { return db.driver }

func (db *DB) Rebind(query string) string {
	if db.driver != DriverPostgres {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (db *DB) LockClause() string {
	if db.driver == DriverPostgres {
		return " FOR UPDATE"
	}
	return ""
}

func (db *DB) IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return isSQLiteCode(err, 2067, 1555)
}

func (db *DB) IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514"
	}
	return isSQLiteCode(err, 275)
}

func (db *DB) IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}
	return isSQLiteCode(err, 787)
}

func isSQLiteCode(err error, codes ...int) bool {
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	for _, code := range codes {
		if sqliteErr.Code() == code {
			return true
		}
	}
	return false
}

func driverName(d Driver) string {
	if d == DriverPostgres {
		return "pgx"
	}
	return "sqlite"
}

func parseDSN(dsn string) (Driver, string, error) {
	if dsn == "" {
		return "", "", errors.New("database DSN is empty")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return DriverPostgres, dsn, nil
	}

	path := strings.TrimPrefix(strings.TrimPrefix(dsn, "sqlite://"), "sqlite:")
	path, query, _ := strings.Cut(path, "?")
	params, err := url.ParseQuery(query)
	if err != nil {
		return "", "", fmt.Errorf("parse sqlite DSN options: %w", err)
	}

	params.Set("_txlock", "immediate")
	params["_pragma"] = append(params["_pragma"],
		"foreign_keys(1)",
		"busy_timeout(5000)",
		"journal_mode(WAL)",
	)
	return DriverSQLite, path + "?" + params.Encode(), nil
}
