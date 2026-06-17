package idempotency

import (
	"fmt"

	"gorm.io/gorm"
)

type IdemRepository struct {
	Db *gorm.DB
}

func (repo *IdemRepository) GetIdempotencyById(idempotency_key string) (*IdempotencyRecord, error) {
	var idempotency IdempotencyRecord

	err := repo.Db.
		Where("idempotency_key = ?", idempotency_key).
		First(&idempotency).
		Error

	if err != nil {
		return nil, err
	}
	return &idempotency, nil
}

func (repo *IdemRepository) UpdateIdempotencyStatus(tx *gorm.DB, idempotency_key string, status string) error {
	query := `
        UPDATE idempotency
        SET status = $1,
            updated_at = NOW()
        WHERE idempotency_key = $2
    `
	err := tx.Exec(query, status, idempotency_key).Error
	return err
}
func (repo *IdemRepository) CreateIdempotencyEntry(tx *gorm.DB, idemp *IdempotencyRecord) error {
	fmt.Println("ye aa rha hain ", idemp)
	err := tx.Create(idemp).Error
	return err
}
