package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/datatypes"
)

func TestIdempotencyRecord_TableName(t *testing.T) {
	record := IdempotencyRecord{}
	assert.Equal(t, "idempotency_records", record.TableName())
}

func TestIdempotencyRecord_Fields(t *testing.T) {
	now := time.Now()
	record := IdempotencyRecord{
		IdempotencyKey: "key-123",
		TransferID:     "transfer-123",
		Response:       datatypes.JSON(`{"status":"PROCESSED"}`),
		CreatedAt:      now,
	}

	assert.Equal(t, "key-123", record.IdempotencyKey)
	assert.Equal(t, "transfer-123", record.TransferID)
	assert.NotNil(t, record.Response)
	assert.Equal(t, now, record.CreatedAt)
}

func TestIdempotencyRecord_WithEmptyResponse(t *testing.T) {
	record := IdempotencyRecord{
		IdempotencyKey: "key-123",
		TransferID:     "transfer-123",
		Response:       datatypes.JSON{},
		CreatedAt:      time.Now(),
	}

	assert.Equal(t, "key-123", record.IdempotencyKey)
	assert.Equal(t, "transfer-123", record.TransferID)
	assert.Equal(t, datatypes.JSON{}, record.Response)
}

func TestIdempotencyRecord_WithNilResponse(t *testing.T) {
	record := IdempotencyRecord{
		IdempotencyKey: "key-123",
		TransferID:     "transfer-123",
		Response:       nil,
		CreatedAt:      time.Now(),
	}

	assert.Equal(t, "key-123", record.IdempotencyKey)
	assert.Equal(t, "transfer-123", record.TransferID)
	assert.Nil(t, record.Response)
}
