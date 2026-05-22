package idempotency

import (
	"time"

	"wallet-transfer-assignment/internal/services/cache"

	"github.com/google/uuid"
)

type IdempotencyControllerImpl struct {
	cacheService cache.CacheHelper
}

func NewIdempotencyControllerImpl(cacheService cache.CacheHelper) IdempotencyController {
	return &IdempotencyControllerImpl{
		cacheService: cacheService,
	}
}

func (b *IdempotencyControllerImpl) GenerateID() (string, error) {
	if err := validateGenerateID(); err != nil {
		return "", err
	}

	newID := uuid.New().String()
	b.cacheService.Set(newID, cache.CacheItem{
		State:     cache.StateIssued,
		CreatedAt: time.Now(),
	})

	return newID, nil
}
