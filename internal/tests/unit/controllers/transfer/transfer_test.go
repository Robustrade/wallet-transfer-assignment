package transfer

import (
	"testing"

	"wallet-transfer-assignment/internal/controller/transfer"
	"wallet-transfer-assignment/internal/models"
	repo_mocks "wallet-transfer-assignment/internal/repositories/mocks"
	cache_mocks "wallet-transfer-assignment/internal/services/cache/mocks"

	"github.com/stretchr/testify/assert"
)

func TestTransferController(t *testing.T) {
	testCases := GetTransferControllerTestCases()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Arrange
			mockRepo := &repo_mocks.MockRepository{}
			mockCache := &cache_mocks.MockCacheHelper{}
			mockContainer := &TransferControllerMocks{
				Repo: mockRepo,
			}
			tc.MockSetup(mockContainer)

			ctrl := transfer.NewTransferControllerImpl(mockRepo, mockCache)

			// Act
			transferObj := &models.Transfer{
				SourceWalletID: tc.SourceWalletID,
				DestWalletID:   tc.DestWalletID,
				Amount:         tc.Amount,
			}
			result, err := ctrl.DoWalletTransfer(transferObj)

			// Assert
			if tc.ExpectErr {
				assert.Error(t, err)
				if tc.ExpectedErrMessage != "" {
					assert.Contains(t, err.Error(), tc.ExpectedErrMessage)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, models.TransferStatusProcessed, result.Status)
			}
		})
	}
}
