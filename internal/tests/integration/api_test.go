package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"wallet-transfer/internal/config"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/observability/metrics"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/tests/testutils"
	"wallet-transfer/internal/worker"
)

// Full-stack behavioral test: HTTP request → handler → service → store → worker
// → terminal result the client polls for.
func TestAPI_EnqueueThenWorkerProcesses(t *testing.T) {
	pool := testutils.NewPool(t)
	from := testutils.CreateWallet(t, pool, "100", true)
	to := testutils.CreateWallet(t, pool, "0", true)

	m := metrics.New()
	store := postgres.New(pool)
	svc := service.NewTransferService(store, zap.NewNop(), m)
	proc := service.NewProcessor(store, zap.NewNop(), 5, m)
	srv := handler.New(config.Config{}, zap.NewNop(), pool, svc, m)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	// Readiness probe reaches the DB.
	rz, err := http.Get(ts.URL + "/readyz")
	require.NoError(t, err)
	rz.Body.Close()
	require.Equal(t, http.StatusOK, rz.StatusCode)

	w := worker.New(store, proc, zap.NewNop(), worker.Config{
		Count: 2, BatchSize: 5, PollInterval: 25 * time.Millisecond,
		ReapInterval: time.Hour, StuckAfter: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = w.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })

	body, _ := json.Marshal(map[string]any{
		"idempotencyKey": "api-" + uuid.NewString(),
		"fromWalletId":   from.String(),
		"toWalletId":     to.String(),
		"amount":         40,
	})
	resp, err := http.Post(ts.URL+"/api/v1/transfers", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	var created struct {
		ID    string `json:"id"`
		State string `json:"state"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	resp.Body.Close()
	require.Equal(t, "PENDING", created.State)

	require.Eventually(t, func() bool {
		r, err := http.Get(ts.URL + "/api/v1/transfers/" + created.ID)
		if err != nil {
			return false
		}
		defer r.Body.Close()
		var got struct {
			State string `json:"state"`
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		return got.State == "PROCESSED"
	}, 5*time.Second, 25*time.Millisecond)

	require.True(t, testutils.Balance(t, pool, from).Equal(testutils.Money("60")))
	require.True(t, testutils.Balance(t, pool, to).Equal(testutils.Money("40")))
}
