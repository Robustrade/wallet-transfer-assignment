package transfer

import (
	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
)

// RepositoryMocks holds the SQL mock and GORM DB for repository testing.
type RepositoryMocks struct {
	SqlMock sqlmock.Sqlmock
	DB      *gorm.DB
}

// RepositoryTestCase defines a test scenario for the repository.
type RepositoryTestCase struct {
	Name               string
	MockSetup          func(m *RepositoryMocks)
	ExpectErr          bool
	ExpectedErrMessage string
}
