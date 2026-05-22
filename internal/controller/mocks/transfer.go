package mocks

import "wallet-transfer-assignment/internal/models"

// MockTransferController is a manual mock for transfer.TransferController
type MockTransferController struct {
	DoWalletTransferFunc func(transfer *models.Transfer) (*models.Transfer, error)
}

func (m *MockTransferController) DoWalletTransfer(transfer *models.Transfer) (*models.Transfer, error) {
	if m.DoWalletTransferFunc != nil {
		return m.DoWalletTransferFunc(transfer)
	}
	return nil, nil
}
