package service

import (
	"bytes"
	"context"
	"errors"
	"time"

	db "github.com/mritunjaysingh22/wallet-transfer-assignment/internal/db"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInsufficientBalance = errors.New("insufficient wallet balance")
	ErrInvalidAmount       = errors.New("amount must be greater than zero")
	ErrSameWallet          = errors.New("source and destination wallets must be different")
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferResponse struct {
	TransferID     string `json:"transferId"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
}

type TransferService struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewTransferService(
	pool *pgxpool.Pool,
	queries *db.Queries,
) *TransferService {

	return &TransferService{
		pool:    pool,
		queries: queries,
	}
}

func toPgUUID(id string) (pgtype.UUID, error) {

	u, err := uuid.Parse(id)
	if err != nil {
		return pgtype.UUID{}, err
	}

	var pg pgtype.UUID
	copy(pg.Bytes[:], u[:])
	pg.Valid = true

	return pg, nil
}

func newUUID() pgtype.UUID {

	u := uuid.New()

	var pg pgtype.UUID
	copy(pg.Bytes[:], u[:])
	pg.Valid = true

	return pg
}

func uuidString(id pgtype.UUID) string {

	if !id.Valid {
		return ""
	}

	u, err := uuid.FromBytes(id.Bytes[:])
	if err != nil {
		return ""
	}

	return u.String()
}

// CreateTransfer will be implemented in Part-2.
// Entire workflow executes inside one PostgreSQL transaction.
//
// Flow:
//
// 1. Begin transaction
// 2. Check idempotency key
// 3. Lock wallets (FOR UPDATE)
// 4. Validate balance
// 5. Create transfer (PENDING)
// 6. Update balances
// 7. Insert DEBIT ledger
// 8. Insert CREDIT ledger
// 9. Update transfer -> PROCESSED
// 10. Commit
func (s *TransferService) createTransferTx(
	ctx context.Context,
	req TransferRequest,
) (*TransferResponse, error) {
	var err error
	// 1. Validate request
	if req.Amount <= 0 {
		return nil, ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return nil, ErrSameWallet
	}

	fromWalletID, err := toPgUUID(req.FromWalletID)
	if err != nil {
		return nil, err
	}

	toWalletID, err := toPgUUID(req.ToWalletID)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}

		// Safe to call even after Commit()
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	qtx := s.queries.WithTx(tx)

	// -------------------------------------------------
	// Idempotency
	// -------------------------------------------------

	existing, err := qtx.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)

	if err == nil {

		return &TransferResponse{
			TransferID:     uuidString(existing.ID),
			IdempotencyKey: existing.IdempotencyKey,
			FromWalletID:   uuidString(existing.FromWalletID),
			ToWalletID:     uuidString(existing.ToWalletID),
			Amount:         existing.Amount,
			Status:         string(existing.Status),
		}, nil
	}

	// Ignore "no rows".
	// Any other error should be returned.
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// -------------------------------------------------
	// Deadlock Prevention
	//
	// Always lock wallets in deterministic order.
	// -------------------------------------------------

	fromWallet, toWallet, err := s.lockWallets(
		ctx,
		qtx,
		fromWalletID,
		toWalletID,
	)
	if err != nil {
		return nil, err
	}
	// -------------------------------------------------
	// Validate Balance
	// -------------------------------------------------

	if fromWallet.Balance < req.Amount {
		return nil, ErrInsufficientBalance
	}

	// -------------------------------------------------
	// Create Transfer (PENDING)
	// -------------------------------------------------

	transferID := newUUID()

	transfer, err := qtx.CreateTransfer(ctx, db.CreateTransferParams{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromWallet.ID,
		ToWalletID:     toWallet.ID,
		Amount:         req.Amount,
		Status:         db.TransferStatusPENDING,
	})
	if err != nil {

		var pgErr *pgconn.PgError

		if errors.As(err, &pgErr) &&
			pgErr.Code == "23505" {

			existing, err := qtx.GetTransferByIdempotencyKey(
				ctx,
				req.IdempotencyKey,
			)
			if err != nil {
				return nil, err
			}

			return &TransferResponse{
				TransferID:     uuidString(existing.ID),
				IdempotencyKey: existing.IdempotencyKey,
				FromWalletID:   uuidString(existing.FromWalletID),
				ToWalletID:     uuidString(existing.ToWalletID),
				Amount:         existing.Amount,
				Status:         string(existing.Status),
			}, nil
		}

		return nil, err
	}

	// -------------------------------------------------
	// Update Source Wallet Balance
	// -------------------------------------------------

	newFromBalance := fromWallet.Balance - req.Amount

	err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID:      fromWallet.ID,
		Balance: newFromBalance,
	})
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------
	// Update Destination Wallet Balance
	// -------------------------------------------------

	newToBalance := toWallet.Balance + req.Amount

	err = qtx.UpdateWalletBalance(ctx, db.UpdateWalletBalanceParams{
		ID:      toWallet.ID,
		Balance: newToBalance,
	})
	if err != nil {
		return nil, err
	}
	// -------------------------------------------------
	// Create DEBIT Ledger Entry
	// -------------------------------------------------

	_, err = qtx.CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
		ID:         newUUID(),
		TransferID: transfer.ID,
		WalletID:   fromWallet.ID,
		EntryType:  db.LedgerTypeDEBIT,
		Amount:     req.Amount,
	})
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------
	// Create CREDIT Ledger Entry
	// -------------------------------------------------

	_, err = qtx.CreateLedgerEntry(ctx, db.CreateLedgerEntryParams{
		ID:         newUUID(),
		TransferID: transfer.ID,
		WalletID:   toWallet.ID,
		EntryType:  db.LedgerTypeCREDIT,
		Amount:     req.Amount,
	})
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------
	// Update Transfer Status
	// -------------------------------------------------

	err = qtx.UpdateTransferStatus(ctx, db.UpdateTransferStatusParams{
		ID:     transfer.ID,
		Status: db.TransferStatusPROCESSED,
	})
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------
	// Commit Transaction
	// -------------------------------------------------
	err = tx.Commit(ctx)
	if err != nil {
		return nil, err
	}

	// -------------------------------------------------
	// Return Response
	// -------------------------------------------------

	return &TransferResponse{
		TransferID:     uuidString(transfer.ID),
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   uuidString(transfer.FromWalletID),
		ToWalletID:     uuidString(transfer.ToWalletID),
		Amount:         transfer.Amount,
		Status:         string(db.TransferStatusPROCESSED),
	}, nil

}

func (s *TransferService) lockWallets(
	ctx context.Context,
	qtx *db.Queries,
	fromID,
	toID pgtype.UUID,
) (db.Wallet, db.Wallet, error) {

	lockSourceFirst := true

	first := fromID
	second := toID

	if bytes.Compare(first.Bytes[:], second.Bytes[:]) > 0 {
		first = toID
		second = fromID
		lockSourceFirst = false
	}

	w1, err := qtx.GetWalletForUpdate(ctx, first)
	if err != nil {
		return db.Wallet{}, db.Wallet{}, err
	}

	w2, err := qtx.GetWalletForUpdate(ctx, second)
	if err != nil {
		return db.Wallet{}, db.Wallet{}, err
	}

	if lockSourceFirst {
		return w1, w2, nil
	}

	return w2, w1, nil
}

func (s *TransferService) CreateTransfer(
	ctx context.Context,
	req TransferRequest,
) (*TransferResponse, error) {

	const maxRetries = 3

	for attempt := 0; attempt < maxRetries; attempt++ {

		resp, err := s.createTransferTx(ctx, req)
		if err == nil {
			return resp, nil
		}

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "40001", // serialization failure
				"40P01": // deadlock detected
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}
		}

		return nil, err
	}

	return nil, errors.New("maximum retry attempts exceeded")
}
