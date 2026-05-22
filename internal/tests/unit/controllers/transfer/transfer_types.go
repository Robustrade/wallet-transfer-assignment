package transfer

import (
	"wallet-transfer-assignment/internal/repositories/mocks"
)

// TransferControllerMocks holds the manual mocks required for the Transfer Controller tests.
type TransferControllerMocks struct {
	Repo *mocks.MockRepository
}

// TransferControllerTestCase defines the structure for table-driven tests.
type TransferControllerTestCase struct {
	Name               string
	SourceWalletID     string
	DestWalletID       string
	Amount             int64
	MockSetup          func(m *TransferControllerMocks)
	ExpectErr          bool
	ExpectedErrMessage string
}
