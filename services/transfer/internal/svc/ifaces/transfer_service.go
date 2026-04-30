// Package svciface declares the public contract of the transfer service.
package svciface

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/response"
)

// TransferServiceIface orchestrates wallet-to-wallet transfers.
type TransferServiceIface interface {
	// CreateTransfer executes (or replays) a wallet-to-wallet transfer.
	//
	// Idempotency: a duplicate request with the same idempotencyKey returns
	// the original outcome (PROCESSED or FAILED). A duplicate request with
	// the same key but a different body returns ErrIdempotencyConflict.
	//
	// Atomicity: the entire transfer (idempotency claim, balance updates,
	// ledger entries, status update) is performed in a single transaction.
	CreateTransfer(ctx context.Context, req request.CreateTransferReq) (response.TransferResp, error)
}
