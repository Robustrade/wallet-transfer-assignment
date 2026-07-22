package tracing_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/observability/tracing"
)

func TestTracing_DisabledIsNoop(t *testing.T) {
	shutdown, err := tracing.Init(context.Background(), tracing.Config{Enabled: false})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestTracing_EnabledInstallsProvider(t *testing.T) {
	shutdown, err := tracing.Init(context.Background(), tracing.Config{
		Enabled: true, Endpoint: "localhost:4317", ServiceName: "test", SampleRatio: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, shutdown(ctx))
}
