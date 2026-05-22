package transfer

import (
	"testing"

	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/repositories"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestTransferRepository(t *testing.T) {
	testCases := GetRepositoryTestCases()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Arrange
			db, mock, err := sqlmock.New()
			assert.NoError(t, err)
			defer db.Close()

			gormDB, err := gorm.Open(postgres.New(postgres.Config{
				Conn: db,
			}), &gorm.Config{})
			assert.NoError(t, err)

			m := &RepositoryMocks{
				SqlMock: mock,
				DB:      gormDB,
			}
			tc.MockSetup(m)

			repo := repositories.NewTransferRepository(gormDB)

			// Act
			validID1 := "1a22ae5d-438b-419c-aafd-5e999ec7f2e4"
			transferObj := &models.Transfer{
				ID:             validID1,
				IdempotencyKey: validID1,
				SourceWalletID: validID1,
				DestWalletID:   "50793c1f-4af0-4cd8-af13-180b8f0d5fe3",
				Amount:         1000,
			}
			result, err := repo.ProcessTransfer(transferObj)

			// Assert
			if tc.ExpectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
