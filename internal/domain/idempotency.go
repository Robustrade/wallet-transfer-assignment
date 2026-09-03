package domain

import "time"

type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  string
	CreatedAt   time.Time
}
