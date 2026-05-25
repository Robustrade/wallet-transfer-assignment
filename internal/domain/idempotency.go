package domain

type IdempotencyKey string

type IdempotencyRecord struct {
	Key        IdempotencyKey
	TransferID TransferID
}
