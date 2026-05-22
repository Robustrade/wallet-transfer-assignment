package idempotency

type IdempotencyController interface {
	GenerateID() (string, error)
}
