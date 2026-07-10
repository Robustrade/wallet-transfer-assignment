package logger_test

import (
	"testing"

	"github.com/rs/zerolog"

	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
)

func TestInit_ValidLevel(t *testing.T) {
	logger.Init("debug")
	if logger.L().GetLevel() != zerolog.DebugLevel {
		t.Errorf("expected debug level, got %s", logger.L().GetLevel())
	}
}

func TestInit_InvalidLevelFallsBackToInfo(t *testing.T) {
	logger.Init("not-a-level")
	if logger.L().GetLevel() != zerolog.InfoLevel {
		t.Errorf("expected fallback to info level, got %s", logger.L().GetLevel())
	}
}

func TestInit_CaseInsensitive(t *testing.T) {
	logger.Init("WARN")
	if logger.L().GetLevel() != zerolog.WarnLevel {
		t.Errorf("expected warn level, got %s", logger.L().GetLevel())
	}
}
