package utils

import "go.uber.org/zap"

type Log struct {
	log *zap.Logger
}

type Logger interface {
	Info(message string, fields ...zap.Field)
	Error(message string, fields ...zap.Field)
	Warn(message string, fields ...zap.Field)
	Debug(message string, fields ...zap.Field)
	Sync()
}

func NewLogger() Logger {
	log, err := zap.NewProduction(
		zap.AddCaller(),
		zap.AddCallerSkip(1),
	)
	if err != nil {
		panic(err)
	}
	return Log{
		log: log,
	}
}

func (l Log) Info(message string, fields ...zap.Field) {
	l.log.Info(message, fields...)
}

func (l Log) Error(message string, fields ...zap.Field) {
	l.log.Error(message, fields...)
}

func (l Log) Warn(message string, fields ...zap.Field) {
	l.log.Warn(message, fields...)
}

func (l Log) Debug(message string, fields ...zap.Field) {
	l.log.Debug(message, fields...)
}

func (l Log) Sync() {
	_ = l.log.Sync()
}
