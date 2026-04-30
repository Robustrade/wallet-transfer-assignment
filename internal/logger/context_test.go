package logger

import (
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestRequestContextFieldsAndLogging(t *testing.T) {
	core, recorded := observer.New(zapcore.DebugLevel)
	Log = zap.New(core)

	rc := NewRequestContext("idem-1")
	if rc.TraceID == "" {
		t.Fatalf("trace id should be generated")
	}

	rc = rc.WithTransferID(99)
	if rc.TransferID != 99 {
		t.Fatalf("transfer id should be set")
	}

	fields := rc.Fields()
	if len(fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(fields))
	}

	rc.Info("info message")
	rc.Warn("warn message")
	rc.Debug("debug message")
	rc.Error("error message", errors.New("boom"))

	if recorded.Len() != 4 {
		t.Fatalf("expected 4 log entries, got %d", recorded.Len())
	}
}
