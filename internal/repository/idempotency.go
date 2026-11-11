package repository

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	apperrors "wallet-transfer-assignment/internal/appErrors"
	"wallet-transfer-assignment/internal/domain"
)

func FindIdempotencyRecord(db *gorm.DB, key string) (domain.IdempotencyRecord, bool, error) {
	var record domain.IdempotencyRecord
	err := db.Table("idempotency_records").Select("idempotency_key,request_hash,transfer_id").Take(&record, "idempotency_key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return record, false, nil
	}
	return record, err == nil, err
}

func CheckWallets(db *gorm.DB, from, to string) error {
	var count int64
	if err := db.Model(&domain.Wallet{}).Where("id IN ?", []string{from, to}).Count(&count).Error; err != nil {
		return err
	}
	if count != 2 {
		return apperrors.ErrWalletNotFound
	}
	return nil
}

func CreatePendingTransfer(tx *gorm.DB, request domain.TransferRequest) (string, error) {
	var id string
	err := tx.Raw(
		"INSERT INTO transfers(from_wallet_id,to_wallet_id,amount) VALUES(?,?,?) RETURNING id",
		request.FromWalletID, request.ToWalletID, request.Amount,
	).Row().Scan(&id)
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23503" {
		return "", fmt.Errorf("%w: %w", apperrors.ErrWalletNotFound, err)
	}
	return id, err
}

// InsertIdempotencyRecord lets PostgreSQL arbitrate simultaneous reservations.
// A false result requires rolling back the tentative transfer in this transaction.
func InsertIdempotencyRecord(tx *gorm.DB, record domain.IdempotencyRecord) (bool, error) {
	result := tx.Exec(
		`INSERT INTO idempotency_records(idempotency_key,request_hash,transfer_id)
		 VALUES(?,?,?) ON CONFLICT (idempotency_key) DO NOTHING`,
		record.IdempotencyKey, record.RequestHash, record.TransferID,
	)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	return true, requireRows(result, 1, "reserve idempotency key")
}
