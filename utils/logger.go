package utils

import "go.uber.org/zap"

type Logger struct {
	Logs *zap.Logger
}

func NewLogger() Logger {
	logs, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return Logger{
		Logs: logs,
	}
}
