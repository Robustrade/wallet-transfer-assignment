package transfer

import (
	"errors"

	"net/http"

	"wallet-transfer-assignment/internal/controller/transfer"
	"wallet-transfer-assignment/internal/models"
)

// GetTransferTestCases returns the list of test scenarios for the Transfer Handler.
func GetTransferTestCases() []TransferTestCase {
	validID1 := "1a22ae5d-438b-419c-aafd-5e999ec7f2e4"
	validID2 := "50793c1f-4af0-4cd8-af13-180b8f0d5fe3"

	return []TransferTestCase{
		{
			Name:               "TC_TX_H_1: Malformed JSON",
			RequestBody:        `{"fromWalletId": "invalid-json"`,
			IdempotencyKey:     "1a22ae5d-438b-419c-aafd-5e999ec7f2e4",
			MockSetup:          func(m *TransferMocks) {},
			ExpectedStatusCode: http.StatusBadRequest,
			ExpectedBodyMatch:  `"error_message"`,
		},
		{
			Name: "TC_TX_H_2: Validation Error - Self Transfer",
			RequestBody: `{"fromWalletId": "` + validID1 + `", "toWalletId": "` +
				validID1 + `", "amount": 1000}`,
			IdempotencyKey: validID1,
			MockSetup: func(m *TransferMocks) {
				m.Controller.DoWalletTransferFunc = func(t *models.Transfer) (*models.Transfer, error) {
					return nil, &transfer.ValidationError{Message: "source and destination wallets must be different"}
				}
			},
			ExpectedStatusCode: http.StatusBadRequest,
			ExpectedBodyMatch:  `"error_message":"source and destination wallets must be different"`,
		},
		{
			Name: "TC_TX_H_3: Business Error - Insufficient Funds",
			RequestBody: `{"fromWalletId": "` + validID1 + `", "toWalletId": "` +
				validID2 + `", "amount": 1000}`,
			IdempotencyKey: validID1,
			MockSetup: func(m *TransferMocks) {
				m.Controller.DoWalletTransferFunc = func(t *models.Transfer) (*models.Transfer, error) {
					return &models.Transfer{ID: validID1, Status: models.TransferStatusFailed},
						&transfer.BusinessError{Message: "insufficient funds"}
				}
			},
			ExpectedStatusCode: http.StatusUnprocessableEntity,
			ExpectedBodyMatch:  `"error_message":"insufficient funds"`,
		},
		{
			Name: "TC_TX_H_4: Successful Transfer",
			RequestBody: `{"fromWalletId": "` + validID1 + `", "toWalletId": "` +
				validID2 + `", "amount": 500}`,
			IdempotencyKey: validID1,
			MockSetup: func(m *TransferMocks) {
				m.Controller.DoWalletTransferFunc = func(t *models.Transfer) (*models.Transfer, error) {
					return &models.Transfer{ID: validID1, Status: models.TransferStatusProcessed}, nil
				}
			},
			ExpectedStatusCode: http.StatusOK,
			ExpectedBodyMatch:  `"status":"PROCESSED"`,
		},
		{
			Name: "TC_TX_H_5: Internal Server Error",
			RequestBody: `{"fromWalletId": "` + validID1 + `", "toWalletId": "` +
				validID2 + `", "amount": 500}`,
			IdempotencyKey: validID1,
			MockSetup: func(m *TransferMocks) {
				m.Controller.DoWalletTransferFunc = func(t *models.Transfer) (*models.Transfer, error) {
					return nil, errors.New("database connection lost")
				}
			},
			ExpectedStatusCode: http.StatusInternalServerError,
			ExpectedBodyMatch:  `"error_message":"database connection lost"`,
		},
	}
}
