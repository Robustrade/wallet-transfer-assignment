// Package postgres provides PostgreSQL-backed implementations of the
// repository interfaces declared in the parent repository package.
//
// Every method takes an Executor (satisfied by both *sql.DB and *sql.Tx)
// so the same methods work inside and outside of a transaction. The
// TxManager in this package is the single transaction primitive used by
// the service layer.
package postgres
