package model

import (
	"time"
)

// var (
// 	ErrIdempotencyKeyExpired = errors.New("idempotency key has expired; please generate a fresh key")
// ) // moving this to common

type IdempotencyStatus string

const (
	IdempotencyStatusStarted   IdempotencyStatus = "STARTED"
	IdempotencyStatusCompleted IdempotencyStatus = "COMPLETED"
	IdempotencyStatusFailed    IdempotencyStatus = "FAILED"
	IdempotencyStatusExpired   IdempotencyStatus = "EXPIRED"
)

type IdempotencyRecord struct {
	Key          string            `json:"key"`
	RequestHash  string            `json:"request_hash"`
	Status       IdempotencyStatus `json:"status"`
	ResponseCode int               `json:"response_code"`
	ResponseBody string            `json:"response_body"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}
