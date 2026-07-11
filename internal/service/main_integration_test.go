//go:build integration

package service

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/idgen"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository/cache"
	pgrepo "github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/testutil"
)

var testPool *pgxpool.Pool
var testRedis *redis.Client

func testRedisAddr() string {
	if addr := os.Getenv("TEST_REDIS_ADDR"); addr != "" {
		return addr
	}
	return "localhost:6379"
}

func TestMain(m *testing.M) {
	ctx := context.Background()
	dsn := testutil.PostgresDSN()

	if err := migrate.Run(dsn, "../../migrations"); err != nil {
		fmt.Fprintln(os.Stderr, "run migrations:", err)
		os.Exit(1)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create pool:", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "ping postgres — is `docker-compose up -d postgres` running?", err)
		os.Exit(1)
	}
	testPool = pool

	redisClient := redis.NewClient(&redis.Options{Addr: testRedisAddr()})
	defer func() { _ = redisClient.Close() }()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		fmt.Fprintln(os.Stderr, "ping redis — is `docker-compose up -d redis` running?", err)
		os.Exit(1)
	}
	testRedis = redisClient

	os.Exit(m.Run())
}

func newIntegrationTransferService(t *testing.T) *TransferService {
	t.Helper()
	gen, err := idgen.NewGenerator(0)
	if err != nil {
		t.Fatalf("new id generator: %v", err)
	}
	return NewTransferService(
		pgrepo.NewTransactor(testPool),
		pgrepo.NewWalletRepo(),
		pgrepo.NewTransferRepo(testPool),
		cache.NewTransferCache(testRedis),
		gen,
		5*time.Second,
		testLogger(),
	)
}
