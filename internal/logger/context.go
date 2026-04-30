package logger

import (
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// RequestContext holds request-scoped logging context
type RequestContext struct {
	TraceID        string
	TransferID     int64
	IdempotencyKey string
}

// NewRequestContext creates a new request context with a generated trace ID
func NewRequestContext(idempotencyKey string) RequestContext {
	return RequestContext{
		TraceID:        uuid.New().String(),
		IdempotencyKey: idempotencyKey,
	}
}

// WithTransferID returns a copy of the context with the transfer ID set
func (rc RequestContext) WithTransferID(transferID int64) RequestContext {
	rc.TransferID = transferID
	return rc
}

// Fields returns zap fields for structured logging
func (rc RequestContext) Fields() []zap.Field {
	fields := []zap.Field{
		zap.String("trace_id", rc.TraceID),
		zap.String("idempotency_key", rc.IdempotencyKey),
	}
	if rc.TransferID > 0 {
		fields = append(fields, zap.Int64("transfer_id", rc.TransferID))
	}
	return fields
}

// Info logs an info level message with request context
func (rc RequestContext) Info(msg string, fields ...zap.Field) {
	Log.Info(msg, append(rc.Fields(), fields...)...)
}

// Error logs an error level message with request context
func (rc RequestContext) Error(msg string, err error, fields ...zap.Field) {
	f := append(rc.Fields(), zap.Error(err))
	Log.Error(msg, append(f, fields...)...)
}

// Warn logs a warning level message with request context
func (rc RequestContext) Warn(msg string, fields ...zap.Field) {
	Log.Warn(msg, append(rc.Fields(), fields...)...)
}

// Debug logs a debug level message with request context
func (rc RequestContext) Debug(msg string, fields ...zap.Field) {
	Log.Debug(msg, append(rc.Fields(), fields...)...)
}
