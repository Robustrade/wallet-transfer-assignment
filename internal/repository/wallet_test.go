package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
)

type queryResult struct {
	contains string
	columns  []string
	values   []driver.Value
	err      error
}

type scriptedConnector struct {
	mu      sync.Mutex
	results []queryResult
}

func (c *scriptedConnector) Connect(context.Context) (driver.Conn, error) {
	return &scriptedConn{connector: c}, nil
}
func (c *scriptedConnector) Driver() driver.Driver { return scriptedDriver{} }

type scriptedDriver struct{}
func (scriptedDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type scriptedConn struct{ connector *scriptedConnector }
func (c *scriptedConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("not implemented") }
func (c *scriptedConn) Close() error { return nil }
func (c *scriptedConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }
func (c *scriptedConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.connector.mu.Lock()
	defer c.connector.mu.Unlock()
	if len(c.connector.results) == 0 {
		return nil, errors.New("unexpected query: " + query)
	}
	result := c.connector.results[0]
	c.connector.results = c.connector.results[1:]
	if !strings.Contains(query, result.contains) {
		return nil, errors.New("query did not contain expected fragment " + result.contains + ": " + query)
	}
	if result.err != nil {
		return nil, result.err
	}
	return &scriptedRows{columns: result.columns, values: result.values}, nil
}

type scriptedRows struct {
	columns []string
	values  []driver.Value
	done    bool
}
func (r *scriptedRows) Columns() []string { return r.columns }
func (r *scriptedRows) Close() error { return nil }
func (r *scriptedRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(dest, r.values)
	return nil
}

func testRepository(t *testing.T, results ...queryResult) *WalletRepository {
	t.Helper()
	connector := &scriptedConnector{results: results}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { _ = db.Close() })
	return NewWalletRepository(db)
}

func walletRow(id, currency string, balance int64) queryResult {
	return queryResult{contains: "RETURNING", columns: []string{"id", "currency", "balance"}, values: []driver.Value{id, currency, balance}}
}

func TestCreateWallet(t *testing.T) {
	repo := testRepository(t, walletRow("w-1", "INR", 500))
	got, err := repo.Create(context.Background(), domain.Wallet{ID: "w-1", Currency: "INR", Balance: 500})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "w-1" || got.Currency != "INR" || got.Balance != 500 {
		t.Fatalf("created wallet = %+v", got)
	}
}

func TestCreateRejectsInvalidWalletBeforeQuery(t *testing.T) {
	repo := testRepository(t)
	_, err := repo.Create(context.Background(), domain.Wallet{ID: "w-1", Currency: "INR", Balance: -1})
	if !errors.Is(err, domain.ErrInvalidBalance) {
		t.Fatalf("Create() error = %v, want %v", err, domain.ErrInvalidBalance)
	}
}

func TestGetByIDAndForUpdate(t *testing.T) {
	for _, tc := range []struct {
		name string
		lock bool
		want string
	}{
		{name: "ordinary read", want: "WHERE id = $1"},
		{name: "row lock", lock: true, want: "FOR UPDATE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t, queryResult{
				contains: tc.want,
				columns: []string{"id", "currency", "balance"},
				values: []driver.Value{"w-1", "INR", int64(100)},
			})
			var got domain.Wallet
			var err error
			if tc.lock {
				got, err = repo.GetByIDForUpdate(context.Background(), "w-1")
			} else {
				got, err = repo.GetByID(context.Background(), "w-1")
			}
			if err != nil || got.Balance != 100 {
				t.Fatalf("wallet = %+v, error = %v", got, err)
			}
		})
	}
}

func TestGetByIDNotFound(t *testing.T) {
	repo := testRepository(t, queryResult{contains: "FROM wallets", err: sql.ErrNoRows})
	_, err := repo.GetByID(context.Background(), "missing")
	if !errors.Is(err, ErrWalletNotFound) {
		t.Fatalf("GetByID() error = %v, want %v", err, ErrWalletNotFound)
	}
}

func TestDebitAndCredit(t *testing.T) {
	tests := []struct {
		name string
		call func(*WalletRepository) (domain.Wallet, error)
		wantQuery string
		wantBalance int64
	}{
		{name: "debit", call: func(r *WalletRepository) (domain.Wallet, error) { return r.Debit(context.Background(), "w-1", 40) }, wantQuery: "balance - $1", wantBalance: 60},
		{name: "credit", call: func(r *WalletRepository) (domain.Wallet, error) { return r.Credit(context.Background(), "w-1", 40) }, wantQuery: "balance + $1", wantBalance: 140},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := testRepository(t, queryResult{
				contains: tc.wantQuery,
				columns: []string{"id", "currency", "balance"},
				values: []driver.Value{"w-1", "INR", tc.wantBalance},
			})
			got, err := tc.call(repo)
			if err != nil || int64(got.Balance) != tc.wantBalance {
				t.Fatalf("wallet = %+v, error = %v", got, err)
			}
		})
	}
}

func TestDebitInsufficientFundsAndMissingWallet(t *testing.T) {
	t.Run("insufficient funds", func(t *testing.T) {
		repo := testRepository(t,
			queryResult{contains: "UPDATE wallets", err: sql.ErrNoRows},
			queryResult{contains: "SELECT EXISTS", columns: []string{"exists"}, values: []driver.Value{true}},
		)
		_, err := repo.Debit(context.Background(), "w-1", 100)
		if !errors.Is(err, domain.ErrInsufficientFunds) {
			t.Fatalf("Debit() error = %v, want %v", err, domain.ErrInsufficientFunds)
		}
	})
	t.Run("missing wallet", func(t *testing.T) {
		repo := testRepository(t,
			queryResult{contains: "UPDATE wallets", err: sql.ErrNoRows},
			queryResult{contains: "SELECT EXISTS", columns: []string{"exists"}, values: []driver.Value{false}},
		)
		_, err := repo.Debit(context.Background(), "missing", 100)
		if !errors.Is(err, ErrWalletNotFound) {
			t.Fatalf("Debit() error = %v, want %v", err, ErrWalletNotFound)
		}
	})
}

func TestRejectsInvalidBalanceAdjustment(t *testing.T) {
	repo := testRepository(t)
	if _, err := repo.Debit(context.Background(), "w-1", 0); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("Debit(0) error = %v", err)
	}
	if _, err := repo.Credit(context.Background(), "w-1", -1); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("Credit(-1) error = %v", err)
	}
}
