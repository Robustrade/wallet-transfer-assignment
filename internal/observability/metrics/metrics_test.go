package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/observability/metrics"
)

func scrape(t *testing.T, m *metrics.Metrics) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func TestMetrics_RecordsAndExposes(t *testing.T) {
	m := metrics.New()
	m.ObserveHTTP("GET", "/api/v1/transfers/{id}", 200, 5*time.Millisecond)
	m.TransferEnqueued()
	m.TransferProcessed("processed", 3*time.Millisecond)
	m.TransferProcessed("failed", time.Millisecond)

	body := scrape(t, m)
	require.Contains(t, body, "transfers_enqueued_total")
	require.Contains(t, body, `transfers_processed_total{result="processed"}`)
	require.Contains(t, body, `transfers_processed_total{result="failed"}`)
	require.Contains(t, body, "http_requests_total")
	require.Contains(t, body, "http_request_duration_seconds")
}

func TestMetrics_DBPoolGauges(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping DB pool metric test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	defer pool.Close()

	m := metrics.New()
	m.RegisterDBPool(pool)
	body := scrape(t, m)
	require.Contains(t, body, "db_pool_max_conns")
	require.Contains(t, body, "db_pool_acquired_conns")
}
