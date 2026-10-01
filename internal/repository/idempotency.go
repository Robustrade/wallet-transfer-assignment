package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/models"
)

type IdempotencyRepository struct{}

func NewIdempotencyRepository() *IdempotencyRepository { return &IdempotencyRepository{} }

func (r *IdempotencyRepository) Claim(db *gorm.DB, key, requestHash string) (bool, error) {
	rec := models.IdempotencyRecord{
		Key:         key,
		RequestHash: requestHash,
		Status:      models.IdempotencyInProgress,
	}
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&rec)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *IdempotencyRepository) Get(db *gorm.DB, key string) (rec *models.IdempotencyRecord, found bool, err error) {
	var recs []models.IdempotencyRecord
	if err := db.Where("idempotency_key = ?", key).Limit(1).Find(&recs).Error; err != nil {
		return nil, false, err
	}
	if len(recs) == 0 {
		return nil, false, nil
	}
	return &recs[0], true, nil
}

// ReclaimStale atomically takes over an IN_PROGRESS row that has not been
// touched for longer than timeout (its owner presumably crashed). It returns
// true only for the one caller whose UPDATE matches the row.
//
// The staleness check is inside the WHERE clause, evaluated by Postgres
// against the live row and its own clock (now()), so two concurrent retries
// cannot both win. Zero rows affected is a normal "you lost" result, not an
// error: re-Get the row and respond to what it says now.
func (r *IdempotencyRepository) ReclaimStale(db *gorm.DB, key string, timeout time.Duration) (bool, error) {
	res := db.Exec(`
		UPDATE idempotency_records
		SET updated_at = now()
		WHERE idempotency_key = ?
		  AND status = 'IN_PROGRESS'
		  AND updated_at < now() - make_interval(secs => ?)`,
		key, timeout.Seconds())
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// Finalize records the outcome and the response to replay on retries. It only
// applies to a row that is still IN_PROGRESS, and returns false if it was not
// (someone else already finalized it).
//
// Call it with the transfer's tx on the success path so the money movement and
// COMPLETED commit or roll back together. If it returns false there, the
// caller must roll back: another attempt already finished this request, and
// committing would apply the transfer twice.
//
// On a business failure (e.g. insufficient funds) nothing moved, so call it
// with the plain db after the transfer transaction has rolled back.
func (r *IdempotencyRepository) Finalize(
	db *gorm.DB,
	key string,
	status models.IdempotencyStatus,
	transferID *uuid.UUID,
	body []byte,
) (bool, error) {

	var tid, bodyArg any
	if transferID != nil {
		tid = *transferID
	}
	if body != nil {
		bodyArg = string(body)
	}

	res := db.Exec(`
		UPDATE idempotency_records
		SET status = ?,
		    transfer_id = ?,
		    response_body = ?::jsonb,
		    updated_at = now()
		WHERE idempotency_key = ?
		  AND status = 'IN_PROGRESS'`,
		string(status), tid, bodyArg, key)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}
