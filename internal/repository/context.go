package repository

import (
	"context"
	"database/sql"
)

type txKey struct{}

// ContextWithTx attaches a transaction to ctx for repository methods.
func ContextWithTx(ctx context.Context, tx *sql.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

// TxFromContext returns the transaction from ctx, or nil if not in a transaction.
func TxFromContext(ctx context.Context) *sql.Tx {
	tx, _ := ctx.Value(txKey{}).(*sql.Tx)
	return tx
}
