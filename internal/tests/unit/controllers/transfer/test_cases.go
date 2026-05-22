package transfer

import (
	"errors"

	"wallet-transfer-assignment/internal/models"
)

// GetTransferControllerTestCases returns the scenarios for the Transfer Controller.
func GetTransferControllerTestCases() []TransferControllerTestCase {
	validID1 := "1a22ae5d-438b-419c-aafd-5e999ec7f2e4"
	validID2 := "50793c1f-4af0-4cd8-af13-180b8f0d5fe3"

	return []TransferControllerTestCase{
		{
			Name:               "TC_TX_C_1: Validation Failure - Self Transfer",
			SourceWalletID:     validID1,
			DestWalletID:       validID1,
			Amount:             1000,
			MockSetup:          func(m *TransferControllerMocks) {},
			ExpectErr:          true,
			ExpectedErrMessage: "source and destination wallets must be different",
		},
		{
			Name:               "TC_TX_C_2: Validation Failure - Low Amount",
			SourceWalletID:     validID1,
			DestWalletID:       validID2,
			Amount:             50,
			MockSetup:          func(m *TransferControllerMocks) {},
			ExpectErr:          true,
			ExpectedErrMessage: "transfer amount must be greater than or equal to 100",
		},
		{
			Name:               "TC_TX_C_3: Validation Failure - High Amount",
			SourceWalletID:     validID1,
			DestWalletID:       validID2,
			Amount:             2000000,
			MockSetup:          func(m *TransferControllerMocks) {},
			ExpectErr:          true,
			ExpectedErrMessage: "transfer amount must be less than or equal to 1,000,000",
		},
		{
			Name:           "TC_TX_C_4: Repository Failure",
			SourceWalletID: validID1,
			DestWalletID:   validID2,
			Amount:         1000,
			MockSetup: func(m *TransferControllerMocks) {
				m.Repo.ProcessTransferFunc = func(t *models.Transfer) (*models.Transfer, error) {
					return nil, errors.New("db error")
				}
			},
			ExpectErr:          true,
			ExpectedErrMessage: "db error",
		},
		{
			Name:           "TC_TX_C_5: Successful Coordination",
			SourceWalletID: validID1,
			DestWalletID:   validID2,
			Amount:         1000,
			MockSetup: func(m *TransferControllerMocks) {
				m.Repo.ProcessTransferFunc = func(t *models.Transfer) (*models.Transfer, error) {
					return &models.Transfer{ID: "tx-1", Status: models.TransferStatusProcessed}, nil
				}
			},
			ExpectErr: false,
		},
	}
}
