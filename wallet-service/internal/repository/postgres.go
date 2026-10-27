package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"

	"wallet-service/internal/domain"
)

var ErrInvariant = errors.New("persistence invariant violated")

// Repository uses one caller-owned transaction. On any error, the caller must
// roll back unless it is intentionally saving a deterministic business failure.
type Repository struct{ tx pgx.Tx }

func New(tx pgx.Tx) *Repository { return &Repository{tx: tx} }

type Result struct {
	TransferID string // Empty for rejections without a transfer.
	HTTPStatus int
	Body       string // Exact serialized response, including whitespace.
}

type Record struct {
	Payload domain.Payload
	Result  Result
}

// ClaimOrLoad returns nil when this transaction owns a newly claimed key.
// An existing record is returned for the service to compare and replay.
func (r *Repository) ClaimOrLoad(ctx context.Context, key string, payload domain.Payload) (*Record, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request identity: %w", err)
	}
	tag, err := r.tx.Exec(ctx, `INSERT INTO idempotency_records (idempotency_key, request_payload)
		VALUES ($1, $2) ON CONFLICT (idempotency_key) DO NOTHING`, key, encoded)
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil, nil
	}
	// A separate statement gets a fresh READ COMMITTED snapshot after the
	// conflicting INSERT has waited for the original transaction to commit.
	var record Record
	var status *int
	var body *string
	err = r.tx.QueryRow(ctx, `SELECT request_payload, coalesce(transfer_id::text, ''), http_status, response_body
		FROM idempotency_records WHERE idempotency_key=$1`, key).
		Scan(&encoded, &record.Result.TransferID, &status, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("claimed key disappeared: %w", ErrInvariant)
	}
	if err != nil {
		return nil, fmt.Errorf("load idempotency result: %w", err)
	}
	if status == nil || body == nil || !json.Valid([]byte(*body)) {
		return nil, fmt.Errorf("incomplete idempotency result: %w", ErrInvariant)
	}
	if err := json.Unmarshal(encoded, &record.Payload); err != nil {
		return nil, fmt.Errorf("invalid stored request identity: %w", ErrInvariant)
	}
	record.Result.HTTPStatus, record.Result.Body = *status, *body
	return &record, nil
}

// LockWallets always acquires locks in Go string order, returning source first.
func (r *Repository) LockWallets(ctx context.Context, fromID, toID string) (domain.Wallet, domain.Wallet, error) {
	var wallets [2]domain.Wallet
	if fromID == toID {
		return wallets[0], wallets[1], domain.ErrSelfTransfer
	}
	ids := []string{fromID, toID}
	sort.Strings(ids)
	for i, id := range ids {
		err := r.tx.QueryRow(ctx, `SELECT id, currency_code, balance_minor FROM wallets WHERE id=$1 FOR UPDATE`, id).
			Scan(&wallets[i].ID, &wallets[i].Currency, &wallets[i].Balance)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Wallet{}, domain.Wallet{}, domain.ErrWalletNotFound
		}
		if err != nil {
			return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("lock wallets: %w", err)
		}
	}
	if wallets[0].ID != fromID {
		wallets[0], wallets[1] = wallets[1], wallets[0]
	}
	return wallets[0], wallets[1], nil
}

func (r *Repository) CreatePending(ctx context.Context, payload domain.Payload, currency string) (domain.Transfer, error) {
	transfer := domain.Transfer{Payload: payload, Currency: currency, Status: domain.Pending}
	err := r.tx.QueryRow(ctx, `INSERT INTO transfers (from_wallet_id, to_wallet_id, currency_code, amount_minor)
		VALUES ($1, $2, $3, $4) RETURNING id::text`, payload.FromWalletID, payload.ToWalletID, currency, payload.Amount).
		Scan(&transfer.ID)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("create pending transfer: %w", err)
	}
	return transfer, nil
}

// UpdateBalances requires both wallets to have been locked by this transaction.
func (r *Repository) UpdateBalances(ctx context.Context, from, to domain.Wallet) error {
	if from.ID == to.ID {
		return domain.ErrSelfTransfer
	}
	tag, err := r.tx.Exec(ctx, `UPDATE wallets
		SET balance_minor=CASE WHEN id=$1 THEN $2::bigint ELSE $4::bigint END
		WHERE id IN ($1, $3)`, from.ID, from.Balance, to.ID, to.Balance)
	if err != nil {
		return fmt.Errorf("update balances: %w", err)
	}
	if tag.RowsAffected() != 2 {
		return fmt.Errorf("update requires two wallets: %w", ErrInvariant)
	}
	return nil
}

// Derive wallets and amounts from the transfer, so the ledger cannot disagree.
func (r *Repository) InsertLedgerPair(ctx context.Context, transferID string) error {
	tag, err := r.tx.Exec(ctx, `INSERT INTO ledger_entries (transfer_id, wallet_id, entry_type, amount_minor)
		SELECT t.id, e.wallet_id, e.entry_type, t.amount_minor
		FROM transfers t CROSS JOIN LATERAL
			(VALUES (t.from_wallet_id, 'DEBIT'), (t.to_wallet_id, 'CREDIT')) AS e(wallet_id, entry_type)
		WHERE t.id=$1 AND t.status='PENDING'`, transferID)
	if err != nil {
		return fmt.Errorf("insert ledger pair: %w", err)
	}
	if tag.RowsAffected() != 2 {
		return fmt.Errorf("ledger requires a pending transfer: %w", ErrInvariant)
	}
	return nil
}

func (r *Repository) CompleteTransfer(ctx context.Context, transferID string, status domain.Status, failureCode string) error {
	if err := domain.ValidateTransition(domain.Pending, status, failureCode); err != nil {
		return err
	}
	tag, err := r.tx.Exec(ctx, `UPDATE transfers SET status=$2, failure_code=nullif($3, ''), completed_at=clock_timestamp()
		WHERE id=$1 AND status='PENDING'`, transferID, string(status), failureCode)
	if err != nil {
		return fmt.Errorf("complete transfer: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return domain.ErrInvalidTransition
	}
	return nil
}

func (r *Repository) SaveResult(ctx context.Context, key string, result Result) error {
	if result.HTTPStatus < 200 || result.HTTPStatus > 599 || !json.Valid([]byte(result.Body)) {
		return fmt.Errorf("invalid saved response: %w", ErrInvariant)
	}
	tag, err := r.tx.Exec(ctx, `UPDATE idempotency_records SET transfer_id=nullif($2, '')::uuid, http_status=$3, response_body=$4
		WHERE idempotency_key=$1 AND http_status IS NULL AND response_body IS NULL`,
		key, result.TransferID, result.HTTPStatus, result.Body)
	if err != nil {
		return fmt.Errorf("save idempotency result: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("result missing or already saved: %w", ErrInvariant)
	}
	return nil
}
