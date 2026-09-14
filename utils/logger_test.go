package utils

import "testing"

func TestNewLoggerImplementsLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("new logger returned nil")
	}
	logger.Info("test info")
	logger.Error("test error")
	logger.Warn("test warning")
	logger.Debug("test debug")
	logger.Sync()
}
