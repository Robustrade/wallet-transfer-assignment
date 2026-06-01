package testhelper

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/testcontainers/testcontainers-go"
	testcontainerspostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres"
)

type DBContainer struct {
	Container testcontainers.Container
	DB        *postgres.DB
	Ctx       context.Context
}

// SetupTestDB spins up a PostgreSQL Docker container and connects to it.
func SetupTestDB() (*DBContainer, error) {
	ctx := context.Background()

	dbName := "wallet_test"
	dbUser := "postgres"
	dbPassword := "postgres"

	postgresContainer, err := testcontainerspostgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		testcontainerspostgres.WithDatabase(dbName),
		testcontainerspostgres.WithUsername(dbUser),
		testcontainerspostgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	log.Printf("Connecting to test db: %s", connStr)

	// Use our internal connection which runs AutoMigrate automatically
	db, err := postgres.NewConnection(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect and migrate db: %w", err)
	}

	return &DBContainer{
		Container: postgresContainer,
		DB:        db,
		Ctx:       ctx,
	}, nil
}

func (c *DBContainer) Teardown() error {
	return c.Container.Terminate(c.Ctx)
}
