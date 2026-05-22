package idempotency

import (
	"testing"

	"wallet-transfer-assignment/internal/controller/idempotency"
	"wallet-transfer-assignment/internal/services/cache"
	"wallet-transfer-assignment/internal/services/cache/mocks"

	"github.com/stretchr/testify/assert"
)

func TestIdempotencyController(t *testing.T) {
	testCases := GetIdempotencyControllerTestCases()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Arrange
			mockCache := &mocks.MockCacheHelper{}
			mockContainer := &IdempotencyControllerMocks{
				Cache: mockCache,
			}

			var capturedKey string
			var capturedState cache.IdempotencyState
			mockCache.SetFunc = func(key string, value cache.CacheItem) {
				capturedKey = key
				capturedState = value.State
			}

			tc.MockSetup(mockContainer)

			ctrl := idempotency.NewIdempotencyControllerImpl(mockCache)

			// Act
			id, err := ctrl.GenerateID()

			// Assert
			if tc.ExpectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, id)
				assert.Equal(t, id, capturedKey)
				assert.Equal(t, cache.StateIssued, capturedState)
			}
		})
	}
}
