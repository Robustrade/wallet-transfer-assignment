// Package service contains the business logic for the wallet-transfer
// service.
//
// It owns transaction boundaries, idempotency, and the wallet lock
// ordering that prevents deadlocks under concurrent transfers. The
// handler layer above and the repository layer below are kept free of
// workflow decisions.
package service
