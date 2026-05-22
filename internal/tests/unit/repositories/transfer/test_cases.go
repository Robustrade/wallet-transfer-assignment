package transfer

import (
	"errors"
	"regexp"

	"wallet-transfer-assignment/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	validID1 = "1a22ae5d-438b-419c-aafd-5e999ec7f2e4"
	validID2 = "50793c1f-4af0-4cd8-af13-180b8f0d5fe3"
)

// GetRepositoryTestCases returns scenarios for the Transfer Repository.
func GetRepositoryTestCases() []RepositoryTestCase {
	return []RepositoryTestCase{
		{
			Name: "TC_TX_R_1: Tier 2 Idempotency - Duplicate Key",
			MockSetup: func(m *RepositoryMocks) {
				// 1. Initial Insert fails with duplicate key error
				m.SqlMock.ExpectBegin()
				m.SqlMock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "transfers"`)).
					WithArgs(
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
					).
					WillReturnError(errors.New(
						"duplicate key value violates unique constraint " +
							"\"transfers_idempotency_key_key\" (SQLSTATE 23505)",
					))
				m.SqlMock.ExpectRollback()

				// 2. Repository should then try to fetch the existing record
				rows := sqlmock.NewRows([]string{"id", "idempotency_key", "status"}).
					AddRow(validID1, validID1, string(models.TransferStatusProcessed))
				m.SqlMock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "transfers" WHERE idempotency_key = $1`)).
					WithArgs(validID1, 1).
					WillReturnRows(rows)
			},
			ExpectErr: false,
		},
		{
			Name: "TC_TX_R_2: Full Successful Transactional Transfer",
			MockSetup: func(m *RepositoryMocks) {
				// 1. Initial Insert (PENDING)
				m.SqlMock.ExpectBegin()
				m.SqlMock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "transfers"`)).
					WithArgs(
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
					).
					WillReturnResult(sqlmock.NewResult(1, 1))
				m.SqlMock.ExpectCommit()

				// 2. Start core transaction
				m.SqlMock.ExpectBegin()

				// 3. Lock Wallets (SELECT FOR UPDATE)
				m.SqlMock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "wallets" WHERE id = $1`)).
					WithArgs(sqlmock.AnyArg(), 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "balance"}).AddRow(validID1, 10000))

				m.SqlMock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "wallets" WHERE id = $1`)).
					WithArgs(sqlmock.AnyArg(), 1).
					WillReturnRows(sqlmock.NewRows([]string{"id", "balance"}).AddRow(validID2, 5000))

				// 4. Update Balances
				m.SqlMock.ExpectExec(regexp.QuoteMeta(`UPDATE "wallets" SET "balance"=$1`)).
					WillReturnResult(sqlmock.NewResult(1, 1))
				m.SqlMock.ExpectExec(regexp.QuoteMeta(`UPDATE "wallets" SET "balance"=$1`)).
					WillReturnResult(sqlmock.NewResult(1, 1))

				// 5. Create Ledgers
				m.SqlMock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "ledgers"`)).
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
				m.SqlMock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO "ledgers"`)).
					WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
					WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(2))

				// 6. Finalize Transfer (Mark PROCESSED)
				m.SqlMock.ExpectExec(regexp.QuoteMeta(`UPDATE "transfers" SET`)).
					WithArgs(
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
						sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
					).
					WillReturnResult(sqlmock.NewResult(1, 1))

				m.SqlMock.ExpectCommit()
			},
			ExpectErr: false,
		},
	}
}
