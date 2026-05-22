package mocks

import "wallet-transfer-assignment/internal/models"

// MockRepository is a manual mock for repositories.Repository
type MockRepository struct {
	ProcessTransferFunc             func(transfer *models.Transfer) (*models.Transfer, error)
	GetTransferByIdempotencyKeyFunc func(key string) (*models.Transfer, error)
}

func (m *MockRepository) ProcessTransfer(transfer *models.Transfer) (*models.Transfer, error) {
	if m.ProcessTransferFunc != nil {
		return m.ProcessTransferFunc(transfer)
	}
	return nil, nil
}

func (m *MockRepository) GetTransferByIdempotencyKey(key string) (*models.Transfer, error) {
	if m.GetTransferByIdempotencyKeyFunc != nil {
		return m.GetTransferByIdempotencyKeyFunc(key)
	}
	return nil, nil
}
