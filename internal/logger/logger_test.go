package logger_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/logger"
)

func TestLogger_New(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		l, err := logger.New(level)
		require.NoError(t, err)
		require.NotNil(t, l)
	}
}

func TestLogger_InvalidLevel(t *testing.T) {
	_, err := logger.New("not-a-level")
	require.Error(t, err)
}
