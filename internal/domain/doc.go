// Package domain contains the pure entities, state machine, and error
// sentinels for the wallet-transfer service.
//
// It has no dependencies on any other internal package and is safe to
// import from the service, repository, and handler layers. The state
// machine for transfers (PENDING -> PROCESSED | FAILED) is the
// authoritative source of allowed transitions.
package domain
