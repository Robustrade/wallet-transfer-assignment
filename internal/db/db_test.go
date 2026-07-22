package db_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/config"
	"wallet-transfer/internal/db"
)

func TestNewPool_InvalidURL(t *testing.T) {
	_, err := db.NewPool(context.Background(), config.Config{DatabaseURL: "://not-a-valid-url", MaxDBConns: 1})
	require.Error(t, err)
}

func TestNewPool_Success(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping")
	}
	pool, err := db.NewPool(context.Background(), config.Config{DatabaseURL: url, MaxDBConns: 5})
	require.NoError(t, err)
	require.NotNil(t, pool)
	pool.Close()
}
