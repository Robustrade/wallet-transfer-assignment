package idempotency

import "gorm.io/gorm"

type IdemService struct {
	Db *gorm.DB
}

func (ser *IdemService) GetIdempotencyById(id string) (*IdempotencyRecord, error) {
	repo := &IdemRepository{Db: ser.Db}
	res, err := repo.GetIdempotencyById(id)
	return res, err
}
