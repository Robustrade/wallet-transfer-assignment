package idempotency

import (
	"wallet-transfer-assignment/internal/controller/mocks"
)

// IdempotencyMocks holds the manual mocks required for the Idempotency Handler tests.
type IdempotencyMocks struct {
	Controller *mocks.MockIdempotencyController
}

// IdempotencyTestCase defines the structure for table-driven tests.
type IdempotencyTestCase struct {
	Name               string
	MockSetup          func(m *IdempotencyMocks)
	ExpectedStatusCode int
	ExpectedBodyMatch  string
}
