package mocks

// MockIdempotencyController is a manual mock for idempotency.IdempotencyController
type MockIdempotencyController struct {
	GenerateIDFunc func() (string, error)
}

func (m *MockIdempotencyController) GenerateID() (string, error) {
	if m.GenerateIDFunc != nil {
		return m.GenerateIDFunc()
	}
	return "", nil
}
