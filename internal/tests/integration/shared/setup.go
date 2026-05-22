package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"wallet-transfer-assignment/internal/api/routes"
	idempotencyController "wallet-transfer-assignment/internal/controller/idempotency"
	transferController "wallet-transfer-assignment/internal/controller/transfer"
	idempotencyHandlers "wallet-transfer-assignment/internal/handlers/idempotency"
	transferHandlers "wallet-transfer-assignment/internal/handlers/transfer"
	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/repositories"
	"wallet-transfer-assignment/internal/services/cache"
	"wallet-transfer-assignment/internal/services/database"

	"github.com/gin-gonic/gin"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	TestRouter     *gin.Engine
	TestDB         *gorm.DB
	TestCache      cache.CacheHelper
	currentConnStr string
)

// ReinitDB re-establishes the GORM connection to the existing container.
// This is used for tests that simulate connection loss by closing the DB.
func ReinitDB() {
	db, err := gorm.Open(gormpostgres.Open(currentConnStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		panic(fmt.Sprintf("failed to re-connect to test database: %v", err))
	}
	TestDB = db
	database.DB = db

	// We MUST re-setup the dependencies because they hold old DB references
	transferRepo := repositories.NewTransferRepository(TestDB)
	idempotencyBiz := idempotencyController.NewIdempotencyControllerImpl(TestCache)
	transferBiz := transferController.NewTransferControllerImpl(transferRepo, TestCache)
	transferHandler := transferHandlers.NewTransferHandler(transferBiz)
	idempotencyHandler := idempotencyHandlers.NewIdempotencyHandler(idempotencyBiz)

	TestRouter = routes.SetupRouter(transferHandler, idempotencyHandler, TestCache)
}

// SetupTestEnvironment spins up the Postgres container, migrates the DB,
// and wires up the dependencies for integration testing.
func SetupTestEnvironment() (func(), error) {
	ctx := context.Background()

	// 1. Start ephemeral PostgreSQL container
	pgContainer, err := postgres.RunContainer(ctx,
		testcontainers.WithImage("postgres:15-alpine"),
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start postgres container: %v", err)
	}

	// 2. Get connection string and connect GORM
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("failed to get connection string: %v", err)
	}
	currentConnStr = connStr

	db, err := gorm.Open(gormpostgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to test database: %v", err)
	}
	TestDB = db
	database.DB = db

	// 3. Initialize Tables and Triggers using SHARED production logic (DRY)
	database.InitializeSchema(db)

	// 4. Setup Router and Dependencies
	TestCache = cache.NewSyncMapCache(1 * time.Hour)
	transferRepo := repositories.NewTransferRepository(TestDB)
	idempotencyBiz := idempotencyController.NewIdempotencyControllerImpl(TestCache)
	transferBiz := transferController.NewTransferControllerImpl(transferRepo, TestCache)
	transferHandler := transferHandlers.NewTransferHandler(transferBiz)
	idempotencyHandler := idempotencyHandlers.NewIdempotencyHandler(idempotencyBiz)

	gin.SetMode(gin.TestMode)
	TestRouter = routes.SetupRouter(transferHandler, idempotencyHandler, TestCache)

	// Return cleanup function
	cleanup := func() {
		_ = pgContainer.Terminate(ctx)
	}

	return cleanup, nil
}

// ClearDatabase wipes all transaction data.
func ClearDatabase() {
	TestDB.Exec("TRUNCATE TABLE wallets, ledgers, transfers CASCADE")
	TestCache.Clear()
}

// SeedWallet ensures a test wallet exists.
func SeedWallet(id string, balance int64) {
	w := models.Wallet{ID: id, Balance: balance}
	TestDB.Save(&w)
}

// PreIssueKey registers a key in the cache so the middleware allows it.
func PreIssueKey(key string) {
	TestCache.Set(key, cache.CacheItem{
		State:     cache.StateIssued,
		CreatedAt: time.Now(),
	})
}

// ExecuteRequest is a helper to run HTTP requests against the test router.
func ExecuteRequest(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	TestRouter.ServeHTTP(rr, req)
	return rr
}

// ExecuteTransferRequest marshals the payload and executes a POST /transfers request.
func ExecuteTransferRequest(payload interface{}, idempotencyKey string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/transfers", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return ExecuteRequest(req)
}
