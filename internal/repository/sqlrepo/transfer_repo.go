package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

type transferRepo struct {
	q Querier
	d Dialect
}

const transferColumns = `id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at`

func (r *transferRepo) Create(ctx context.Context, t *domain.Transfer) error {
	createdAt := FormatTime(t.CreatedAt)
	_, err := r.q.ExecContext(ctx, r.d.Rebind(
		`INSERT INTO transfers (`+transferColumns+`, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount,
		string(t.Status), nullableReason(t.FailureReason), createdAt, createdAt)
	if err != nil {
		return fmt.Errorf("insert transfer %s: %w", t.ID, err)
	}
	return nil
}

func (r *transferRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Transfer, error) {
	row := r.q.QueryRowContext(ctx, r.d.Rebind(`SELECT `+transferColumns+` FROM transfers WHERE id = ?`), id)
	t, err := scanTransfer(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("transfer %s: %w", id, service.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get transfer %s: %w", id, err)
	}
	return t, nil
}

// UpdateStatus is the guarded transition. The WHERE status = 'PENDING'
// condition means only one writer can ever finalize a transfer. A stale or
// duplicate writer matches zero rows and gets ErrInvalidTransition. This is
// the database-level counterpart of Transfer.requirePending in the domain.
func (r *transferRepo) UpdateStatus(ctx context.Context, t *domain.Transfer, updatedAt time.Time) error {
	res, err := r.q.ExecContext(ctx, r.d.Rebind(
		`UPDATE transfers SET status = ?, failure_reason = ?, updated_at = ?
		 WHERE id = ? AND status = 'PENDING'`),
		string(t.Status), nullableReason(t.FailureReason), FormatTime(updatedAt), t.ID)
	if err != nil {
		return fmt.Errorf("update transfer %s status: %w", t.ID, err)
	}
	return expectOneRow(res, fmt.Errorf("transfer %s -> %s: %w", t.ID, t.Status, domain.ErrInvalidTransition))
}

// ListByWallet returns the newest transfers where the wallet is sender or
// receiver.
//
// It is written as two index-ordered branches merged with UNION ALL, rather
// than "WHERE from_wallet_id = ? OR to_wallet_id = ?". With OR, Postgres
// fetches EVERY transfer of the wallet (BitmapOr) and then sorts them all, to
// return just `limit` rows. Instead, each branch walks its own index
// ((from_wallet_id, created_at) or (to_wallet_id, created_at)) backwards,
// reads only the newest `limit` rows, and the outer query merges the two
// short lists. Measured on Postgres with 300k transfers (60k for one wallet):
// about 31 ms with OR, 0.34 ms with this version.
//
// UNION ALL cannot duplicate a row: CHECK (from_wallet_id <> to_wallet_id)
// means no transfer appears in both branches. The id tie-break makes the order
// deterministic when two transfers share a timestamp. UUIDv7 ids increase over
// time, so the later one sorts first. The branches are wrapped in derived
// tables because SQLite does not allow ORDER BY/LIMIT on a bare UNION member.
func (r *transferRepo) ListByWallet(ctx context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	rows, err := r.q.QueryContext(ctx, r.d.Rebind(
		`SELECT `+transferColumns+` FROM (
		     SELECT `+transferColumns+` FROM (
		         SELECT `+transferColumns+` FROM transfers WHERE from_wallet_id = ?
		         ORDER BY created_at DESC, id DESC LIMIT ?
		     ) sent
		     UNION ALL
		     SELECT `+transferColumns+` FROM (
		         SELECT `+transferColumns+` FROM transfers WHERE to_wallet_id = ?
		         ORDER BY created_at DESC, id DESC LIMIT ?
		     ) received
		 ) wallet_transfers
		 ORDER BY created_at DESC, id DESC
		 LIMIT ?`),
		walletID, limit, walletID, limit, limit)
	if err != nil {
		return nil, fmt.Errorf("list transfers for wallet %s: %w", walletID, err)
	}
	defer rows.Close()

	transfers := []domain.Transfer{} // non-nil so it encodes as [] rather than null
	for rows.Next() {
		t, err := scanTransfer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan transfer: %w", err)
		}
		transfers = append(transfers, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list transfers for wallet %s: %w", walletID, err)
	}
	return transfers, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanTransfer(s rowScanner) (*domain.Transfer, error) {
	var (
		t      domain.Transfer
		status string
		reason sql.NullString
	)
	if err := s.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount,
		&status, &reason, scanTime(&t.CreatedAt)); err != nil {
		return nil, err
	}
	t.Status = domain.Status(status)
	t.FailureReason = domain.FailureReason(reason.String) // "" when NULL
	return &t, nil
}

// nullableReason stores "no reason" as SQL NULL, which the
// "reason iff FAILED" CHECK constraint requires.
func nullableReason(r domain.FailureReason) any {
	if r == "" {
		return nil
	}
	return string(r)
}
