package domain

type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	TransferID     string
}
