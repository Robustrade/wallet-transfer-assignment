package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
	repoiface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/repo/iface"
)

// walletRepository is the GORM-backed implementation of WalletRepoIface.
type walletRepository struct {
	db *gorm.DB
}

// NewWalletRepository constructs a wallet repository.
func NewWalletRepository(db *gorm.DB) repoiface.WalletRepoIface {
	return &walletRepository{db: db}
}

// LockByIDs locks all requested wallet rows in a single round-trip, in
// lexicographic id order. Lex ordering is what makes concurrent transfers
// over the same pair deadlock-free.
func (r *walletRepository) LockByIDs(ctx context.Context, ids []string) (model.Wallets, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Sort caller-supplied IDs to make the lock-acquisition order deterministic
	// even if the caller forgot. (We also rely on the SQL `ORDER BY id`.)
	sortedIDs := make([]string, len(ids))
	copy(sortedIDs, ids)
	sort.Strings(sortedIDs)

	q, err := mustTxQuerier(ctx)
	if err != nil {
		return nil, err
	}

	// FOR NO KEY UPDATE — not FOR UPDATE — is the correct row-level lock here.
	//
	// `INSERT INTO transfers (..., from_wallet_id, to_wallet_id, ...)` runs
	// before this SELECT inside the same transaction (Claim first, then lock).
	// That INSERT's foreign-key check on transfers.from_wallet_id /
	// transfers.to_wallet_id acquires FOR KEY SHARE on the referenced wallet
	// rows. FOR UPDATE conflicts with FOR KEY SHARE; FOR NO KEY UPDATE does
	// not. Concurrent transfers that each hold FOR KEY SHARE on the same
	// wallet (from their own FK check) would all then queue on FOR UPDATE
	// and deadlock (Postgres SQLSTATE 40P01). FOR NO KEY UPDATE breaks the
	// cycle without weakening write serialization: it is mutually exclusive
	// with itself, so two concurrent debits on the same wallet still
	// serialize correctly.
	//
	// We never UPDATE wallets.id (the only "key" column), so the "no key"
	// part is honest — FOR NO KEY UPDATE gives us all the write-side
	// guarantees we actually need.
	const sqlText = `
SELECT id, balance_minor, currency, created_at, updated_at
FROM wallets
WHERE id = ANY($1)
ORDER BY id
FOR NO KEY UPDATE`

	rows, err := q.Raw(sqlText, sortedIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("wallet_repo: lock by ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(model.Wallets, 0, len(sortedIDs))
	for rows.Next() {
		var (
			w       model.Wallet
			balance int64
		)
		if err := rows.Scan(&w.ID, &balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("wallet_repo: scan: %w", err)
		}
		w.Balance = money.FromMinor(balance)
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("wallet_repo: rows: %w", err)
	}

	if len(out) != len(sortedIDs) {
		return nil, apperr.ErrWalletNotFound
	}
	return out, nil
}

// ApplyBalanceDeltas applies all supplied deltas in a single UPDATE,
// using a CASE expression to fan the per-row delta out by id. Saves a
// round-trip on every transfer relative to one UPDATE per wallet.
//
// The DB CHECK (balance_minor >= 0) is the last-line invariant.
func (r *walletRepository) ApplyBalanceDeltas(ctx context.Context, deltas []repoiface.BalanceDelta) error {
	if len(deltas) == 0 {
		return nil
	}
	q, err := mustTxQuerier(ctx)
	if err != nil {
		return err
	}

	// Build:  UPDATE wallets
	//         SET balance_minor = balance_minor + CASE id
	//                 WHEN $1 THEN $2::bigint
	//                 WHEN $3 THEN $4::bigint ... END,
	//             updated_at = NOW()
	//         WHERE id IN ($1, $3, ...)
	//
	// The `::bigint` casts on the THEN branches are required: Postgres can't
	// infer the parameter type inside a CASE expression and would otherwise
	// resolve them as `text`, causing `bigint + text` (42883) at execute time.
	var (
		sb       strings.Builder
		args     = make([]any, 0, len(deltas)*2)
		idArgsCS strings.Builder
	)
	sb.WriteString("UPDATE wallets SET balance_minor = balance_minor + CASE id")
	for i, d := range deltas {
		idIdx := i*2 + 1
		valIdx := i*2 + 2
		fmt.Fprintf(&sb, " WHEN $%d THEN $%d::bigint", idIdx, valIdx)
		args = append(args, d.WalletID, d.Delta.Minor())
		if i > 0 {
			idArgsCS.WriteString(", ")
		}
		fmt.Fprintf(&idArgsCS, "$%d", idIdx)
	}
	sb.WriteString(" END, updated_at = NOW() WHERE id IN (")
	sb.WriteString(idArgsCS.String())
	sb.WriteString(")")

	res := q.Exec(sb.String(), args...)
	if res.Error != nil {
		// Only the balance_minor CHECK can fire from this UPDATE — the
		// statement doesn't touch `currency`, so the currency CHECK can't be
		// re-evaluated. We pin the mapping to that specific constraint so a
		// future schema change that adds another CHECK doesn't silently start
		// surfacing as INSUFFICIENT_FUNDS. Anything else falls through to a
		// generic 5xx, which is the right answer for an unexpected schema
		// rule firing.
		if isPgConstraintViolation(res.Error, sqlstateCheckViolation, constraintWalletsBalanceCheck) {
			return apperr.ErrInsufficientFunds.WithWrap(res.Error)
		}
		return fmt.Errorf("wallet_repo: apply balance deltas: %w", res.Error)
	}
	if res.RowsAffected != int64(len(deltas)) {
		return apperr.ErrWalletNotFound
	}
	return nil
}
