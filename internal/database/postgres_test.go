package database

import (
	"testing"

	"gorm.io/gorm/logger"
)

func TestGormLogLevel(t *testing.T) {
	tests := map[string]logger.LogLevel{
		"silent":  logger.Silent,
		"error":   logger.Error,
		"warn":    logger.Warn,
		"info":    logger.Info,
		"":        logger.Warn, // unset: never log every statement by default
		"unknown": logger.Warn,
	}

	for in, want := range tests {
		if got := gormLogLevel(in); got != want {
			t.Errorf("gormLogLevel(%q) = %v, want %v", in, got, want)
		}
	}
}
