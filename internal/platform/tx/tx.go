// Package tx defines the minimal interfaces for database transaction management.
// These are platform-level contracts implemented by concrete storage adapters
// (e.g. internal/platform/postgres) and consumed by domain services that own
// transaction boundaries (e.g. ledger.Service).
//
// No DB driver is imported here — the interfaces are pure Go.
package tx

import "context"

type txKey struct{}

// InjectTx places a transaction-related object into the context.
func InjectTx(ctx context.Context, tx any) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// ExtractTx retrieves the transaction object from the context.
func ExtractTx(ctx context.Context) any {
	return ctx.Value(txKey{})
}

// Starter abstracts the ability to begin a database transaction.
// The concrete implementation lives in internal/platform/postgres.
type Starter interface {
	BeginTx(ctx context.Context) (context.Context, Tx, error)
}

// Tx represents an in-progress database transaction.
type Tx interface {
	Commit() error
	Rollback() error
}
