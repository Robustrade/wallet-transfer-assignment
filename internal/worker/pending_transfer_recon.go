package worker

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
)

const defaultInterval = 3 * time.Minute

// PendingTransferRecon periodically reconciles transfers stuck in PENDING -
// e.g. left behind by a crash between phase 1 (PENDING committed) and
// phase 2 (finalize) of TransferRepository.ExecuteTransfer - by
// re-attempting phase 2 through TransferRepo.ResolvePendingTransfer. Each
// one is marked PROCESSED with real ledger entries if the current balance
// allows it, or FAILED otherwise: the same outcome the original request
// would have produced had it completed, not an arbitrary choice.
type PendingTransferRecon struct {
	TransferRepo domain.ITransferRepository
	// Interval between runs. Defaults to 3 minutes if zero.
	Interval time.Duration

	running atomic.Bool
}

// Run blocks, ticking every r.Interval until ctx is done. If a previous
// run is still in progress when the next one fires (e.g. an unusually
// large backlog), the new run is skipped rather than executing
// concurrently with it - resolving a given transfer is already safe to run
// from two callers at once (see finalizePendingTransfer's locking), but
// there's no reason to pay for overlapping full table scans.
func (r *PendingTransferRecon) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = defaultInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce performs a single reconciliation pass: list PENDING transfers and
// attempt to resolve each one. Exported so it can be triggered directly
// (tests, an admin endpoint) without waiting for the next tick. If a run is
// already in progress, this call skips immediately rather than running
// concurrently with it.
func (r *PendingTransferRecon) RunOnce(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		logger.Debug().Msg("pending-transfer-recon: previous run still in progress, skipping this tick")
		return
	}
	defer r.running.Store(false)

	pending, err := r.TransferRepo.ListPendingTransfers(ctx)
	if err != nil {
		logger.Error().Err(err).Msg("pending-transfer-recon: failed to list pending transfers")
		return
	}
	if len(pending) == 0 {
		return
	}

	logger.Info().Int("count", len(pending)).Msg("pending-transfer-recon: reconciling stuck pending transfer(s)")
	for _, transfer := range pending {
		resolved, err := r.TransferRepo.ResolvePendingTransfer(ctx, transfer)
		if err != nil {
			logger.Error().Err(err).Str("transfer_id", transfer.ID).Msg("pending-transfer-recon: failed to resolve transfer")
			continue
		}
		logger.Info().Str("transfer_id", resolved.ID).Str("status", string(resolved.Status)).Msg("pending-transfer-recon: resolved transfer")
	}
}
