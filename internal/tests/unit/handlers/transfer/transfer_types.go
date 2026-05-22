package transfer

import (
	"wallet-transfer-assignment/internal/controller/mocks"
)

// TransferMocks holds the manual mocks required for the Transfer Handler tests.
type TransferMocks struct {
	Controller *mocks.MockTransferController
}

// TransferTestCase defines the structure for table-driven tests.
type TransferTestCase struct {
	Name               string
	RequestBody        string
	IdempotencyKey     string
	MockSetup          func(m *TransferMocks)
	ExpectedStatusCode int
	ExpectedBodyMatch  string
}
