package domain

type IdempotencyRecord struct {
	Key         string
	RequestHash string
	TransferID  string
}
