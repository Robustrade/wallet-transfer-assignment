package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
)

//go:embed migrations/001_initial.sql
var initialMigration string

type PostgresRepository struct{ db *sql.DB }

func OpenPostgres(ctx context.Context, databaseURL string) (*PostgresRepository, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	r := &PostgresRepository{db: db}
	if err := r.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}
func (r *PostgresRepository) Close() error { return r.db.Close() }
func (r *PostgresRepository) migrate(ctx context.Context) error {
	for _, statement := range strings.Split(initialMigration, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}
func (r *PostgresRepository) CreateWallet(ctx context.Context, id string, balance int64) error {
	if id == "" || balance < 0 {
		return fmt.Errorf("wallet id and non-negative balance are required")
	}
	_, err := r.db.ExecContext(ctx, "INSERT INTO wallets(id, balance) VALUES($1, $2)", id, balance)
	return err
}
func (r *PostgresRepository) Balance(ctx context.Context, id string) (int64, error) {
	var balance int64
	err := r.db.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id=$1", id).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, domain.ErrWalletNotFound
	}
	return balance, err
}

func (r *PostgresRepository) CreateTransfer(ctx context.Context, req domain.CreateTransferRequest) (domain.Transfer, error) {
	fingerprint := requestFingerprint(req)
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return domain.Transfer{}, err
	}
	defer tx.Rollback()
	if existing, found, err := getByKey(ctx, tx, req.IdempotencyKey); err != nil {
		return domain.Transfer{}, err
	} else if found {
		if existing.fingerprint != fingerprint {
			return domain.Transfer{}, domain.ErrIdempotencyConflict
		}
		if err := tx.Commit(); err != nil {
			return domain.Transfer{}, err
		}
		existing.transfer.Replayed = true
		if existing.transfer.Status == domain.Failed {
			return existing.transfer, domain.ErrInsufficientFunds
		}
		return existing.transfer, nil
	}
	id, err := newTransferID()
	if err != nil {
		return domain.Transfer{}, err
	}
	now := time.Now().UTC()
	transfer := domain.Transfer{ID: id, IdempotencyKey: req.IdempotencyKey, FromWalletID: req.FromWalletID, ToWalletID: req.ToWalletID, Amount: req.Amount, Status: domain.Pending}
	if _, err := tx.ExecContext(ctx, `INSERT INTO transfers(id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount, domain.Pending, now); err != nil {
		if isUniqueViolation(err) {
			return r.replayAfterDuplicate(ctx, req.IdempotencyKey, fingerprint)
		}
		return domain.Transfer{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(idempotency_key,request_fingerprint,transfer_id,created_at) VALUES($1,$2,$3,$4)`, req.IdempotencyKey, fingerprint, id, now); err != nil {
		return domain.Transfer{}, err
	}

	balances, err := lockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return domain.Transfer{}, err
	}
	if balances[req.FromWalletID] < req.Amount {
		transfer.Status, transfer.FailureReason = domain.Failed, domain.ErrInsufficientFunds.Error()
		if _, err := tx.ExecContext(ctx, "UPDATE transfers SET status=$1, failure_reason=$2 WHERE id=$3", transfer.Status, transfer.FailureReason, id); err != nil {
			return domain.Transfer{}, err
		}
		if err := tx.Commit(); err != nil {
			return domain.Transfer{}, err
		}
		return transfer, domain.ErrInsufficientFunds
	}
	if _, err := tx.ExecContext(ctx, "UPDATE wallets SET balance=balance-$1 WHERE id=$2", req.Amount, req.FromWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE wallets SET balance=balance+$1 WHERE id=$2", req.Amount, req.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries(transfer_id,wallet_id,entry_type,amount,created_at) VALUES($1,$2,'DEBIT',$3,$4),($1,$5,'CREDIT',$3,$4)`, id, req.FromWalletID, req.Amount, now, req.ToWalletID); err != nil {
		return domain.Transfer{}, err
	}
	transfer.Status = domain.Processed
	if _, err := tx.ExecContext(ctx, "UPDATE transfers SET status=$1 WHERE id=$2 AND status=$3", transfer.Status, id, domain.Pending); err != nil {
		return domain.Transfer{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Transfer{}, err
	}
	return transfer, nil
}

func (r *PostgresRepository) replayAfterDuplicate(ctx context.Context, key, fingerprint string) (domain.Transfer, error) {
	existing, found, err := getByKey(ctx, r.db, key)
	if err != nil {
		return domain.Transfer{}, err
	}
	if !found {
		return domain.Transfer{}, fmt.Errorf("idempotency record disappeared after unique constraint violation")
	}
	if existing.fingerprint != fingerprint {
		return domain.Transfer{}, domain.ErrIdempotencyConflict
	}
	existing.transfer.Replayed = true
	if existing.transfer.Status == domain.Failed {
		return existing.transfer, domain.ErrInsufficientFunds
	}
	return existing.transfer, nil
}
func lockWallets(ctx context.Context, tx *sql.Tx, first, second string) (map[string]int64, error) {
	ids := []string{first, second}
	sort.Strings(ids)
	balances := make(map[string]int64, 2)
	for _, id := range ids {
		var balance int64
		err := tx.QueryRowContext(ctx, "SELECT balance FROM wallets WHERE id=$1 FOR UPDATE", id).Scan(&balance)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		if err != nil {
			return nil, err
		}
		balances[id] = balance
	}
	return balances, nil
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type storedTransfer struct {
	transfer    domain.Transfer
	fingerprint string
}

func getByKey(ctx context.Context, q rowQueryer, key string) (storedTransfer, bool, error) {
	var s storedTransfer
	err := q.QueryRowContext(ctx, `SELECT t.id,t.idempotency_key,t.from_wallet_id,t.to_wallet_id,t.amount,t.status,COALESCE(t.failure_reason,''),i.request_fingerprint FROM transfers t JOIN idempotency_records i ON i.transfer_id=t.id WHERE i.idempotency_key=$1`, key).Scan(&s.transfer.ID, &s.transfer.IdempotencyKey, &s.transfer.FromWalletID, &s.transfer.ToWalletID, &s.transfer.Amount, &s.transfer.Status, &s.transfer.FailureReason, &s.fingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return storedTransfer{}, false, nil
	}
	return s, err == nil, err
}
func requestFingerprint(r domain.CreateTransferRequest) string {
	sum := sha256.Sum256([]byte(r.FromWalletID + "\\x00" + r.ToWalletID + "\\x00" + fmt.Sprint(r.Amount)))
	return hex.EncodeToString(sum[:])
}
func newTransferID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "tr_" + hex.EncodeToString(b[:]), nil
}
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
func (r *PostgresRepository) LedgerEntryCount(ctx context.Context, transferID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, "SELECT count(*) FROM ledger_entries WHERE transfer_id=$1", transferID).Scan(&n)
	return n, err
}
