package logger

import (
	"go.uber.org/zap"
)

var Log *zap.Logger = zap.NewNop() // default no-op logger

func Init() {
	config := zap.NewProductionConfig()
	config.DisableStacktrace = true

	log, _ := config.Build()
	Log = log
}
