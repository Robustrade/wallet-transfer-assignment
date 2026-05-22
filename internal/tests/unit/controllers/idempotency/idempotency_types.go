package idempotency

import (
	"wallet-transfer-assignment/internal/services/cache/mocks"
)

// IdempotencyControllerMocks holds the manual mocks required for the Idempotency Controller tests.
type IdempotencyControllerMocks struct {
	Cache *mocks.MockCacheHelper
}

// IdempotencyControllerTestCase defines the structure for table-driven tests.
type IdempotencyControllerTestCase struct {
	Name      string
	MockSetup func(m *IdempotencyControllerMocks)
	ExpectErr bool
}
