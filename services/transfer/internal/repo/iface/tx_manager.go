package repoiface

import "context"

// TxManager runs the supplied function inside a single database transaction.
// The function receives a derived context that carries the transaction handle;
// repository methods invoked with that context will participate in the txn.
//
// Implementations must commit on a nil return and roll back otherwise.
type TxManager interface {
	Run(ctx context.Context, fn func(ctx context.Context) error) error
}
