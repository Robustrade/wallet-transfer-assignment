package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.uber.org/zap"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// tracer for transfer processing spans (does nothing unless tracing is enabled).
var tracer = otel.Tracer("wallet-transfer/service")

// Processor executes one claimed transfer to a terminal state. It is invoked by
// the worker (Milestone 8) for each claimed PENDING->PROCESSING transfer.
type Processor struct {
	store       repository.Store
	log         *zap.Logger
	maxAttempts int
	retry       retryPolicy
	rec         Recorder
}

func NewProcessor(store repository.Store, log *zap.Logger, maxAttempts int, rec Recorder) *Processor {
	return &Processor{
		store:       store,
		log:         log,
		maxAttempts: maxAttempts,
		retry:       defaultRetryPolicy(),
		rec:         rec,
	}
}

// Process runs a single transfer inside one transaction and returns:
//   - nil       when the transfer is finished (PROCESSED or FAILED). Business
//     failures such as not-enough-funds are saved as FAILED and count as finished.
//   - an error  for a temporary failure. The transfer stays PROCESSING and is
//     retried later by the reaper or the worker.
//
// Telling these two apart is the key rule: a business failure is final and safe
// to repeat, while a temporary failure never leaves the request in a bad state.
func (p *Processor) Process(ctx context.Context, claimed domain.Transfer) error {
	start := time.Now()

	ctx, span := tracer.Start(ctx, "transfer.process")
	span.SetAttributes(attribute.String("transfer.id", claimed.ID.String()))
	defer span.End()

	// If a transfer has been picked up too many times it keeps failing, so stop
	// retrying and mark it FAILED instead of looping forever.
	if claimed.Attempts > p.maxAttempts {
		p.log.Warn("transfer exceeded max attempts, giving up",
			zap.String("transfer_id", claimed.ID.String()), zap.Int("attempts", claimed.Attempts))
		if err := p.terminalFail(ctx, claimed.ID, "max processing attempts exceeded"); err != nil {
			return err
		}
		span.SetAttributes(attribute.String("transfer.result", resultFailed))
		p.rec.TransferProcessed(resultFailed, time.Since(start))
		return nil
	}

	var result string
	err := p.retry.do(ctx, func() error {
		return p.store.Tx(ctx, func(tx repository.TxStore) error {
			r, e := p.execute(ctx, tx, claimed.ID)
			result = r
			return e
		})
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "processing failed")
		p.log.Warn("transfer processing failed, will retry",
			zap.String("transfer_id", claimed.ID.String()), zap.Error(err))
		return err
	}
	if result != "" {
		span.SetAttributes(attribute.String("transfer.result", result))
		p.rec.TransferProcessed(result, time.Since(start))
	}
	return nil
}

// execute does the transfer's work inside an open transaction and reports the
// result ("processed", "failed", or "" when there was nothing to do). An error it
// returns rolls the transaction back; returning nil commits.
func (p *Processor) execute(ctx context.Context, tx repository.TxStore, id uuid.UUID) (string, error) {
	tr, err := tx.GetTransferForUpdate(ctx, id)
	if err != nil {
		return "", err
	}
	// Another worker or the reaper already advanced this transfer. Nothing to do.
	if tr.State != domain.StateProcessing {
		p.log.Debug("transfer no longer processing, skipping",
			zap.String("transfer_id", id.String()), zap.String("state", string(tr.State)))
		return "", nil
	}

	// Lock both wallets. LockWalletPair always locks them in the same order.
	from, to, err := tx.LockWalletPair(ctx, tr.FromWalletID, tr.ToWalletID)
	if err != nil {
		if isBusinessFailure(err) { // for example, a wallet no longer exists
			return resultFailed, tx.SetFailed(ctx, tr.ID, err.Error())
		}
		return "", err
	}

	// Check the business rules while the wallets are locked. If they fail, save the
	// transfer as FAILED so sending the same request again gives the same result.
	if berr := checkBusiness(from, to, tr.Amount); berr != nil {
		p.log.Info("transfer failed business validation",
			zap.String("transfer_id", tr.ID.String()), zap.Error(berr))
		return resultFailed, tx.SetFailed(ctx, tr.ID, berr.Error())
	}

	// Write the ledger entries before touching balances, so if this transfer was
	// already processed we catch it here before changing any balance.
	if err := p.postLedger(ctx, tx, tr); err != nil {
		if errors.Is(err, repository.ErrLedgerDuplicate) {
			// Already processed by an earlier run. Set the state and report it as
			// processed so the completion still shows up in metrics and traces.
			return resultProcessed, tx.SetProcessed(ctx, tr.ID)
		}
		return "", err
	}
	if err := tx.ApplyBalanceDelta(ctx, tr.FromWalletID, tr.Amount.Neg()); err != nil {
		return "", err
	}
	if err := tx.ApplyBalanceDelta(ctx, tr.ToWalletID, tr.Amount); err != nil {
		return "", err
	}
	return resultProcessed, tx.SetProcessed(ctx, tr.ID)
}

// postLedger writes the two ledger entries (one debit, one credit).
func (p *Processor) postLedger(ctx context.Context, tx repository.TxStore, tr domain.Transfer) error {
	if err := tx.InsertLedgerEntry(ctx, domain.LedgerEntry{
		WalletID: tr.FromWalletID, TransferID: tr.ID, Type: domain.EntryDebit, Amount: tr.Amount,
	}); err != nil {
		return err
	}
	return tx.InsertLedgerEntry(ctx, domain.LedgerEntry{
		WalletID: tr.ToWalletID, TransferID: tr.ID, Type: domain.EntryCredit, Amount: tr.Amount,
	})
}

// terminalFail marks a transfer FAILED in its own transaction (the give-up path).
func (p *Processor) terminalFail(ctx context.Context, id uuid.UUID, reason string) error {
	return p.store.Tx(ctx, func(tx repository.TxStore) error {
		tr, err := tx.GetTransferForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if tr.State != domain.StateProcessing {
			return nil
		}
		return tx.SetFailed(ctx, id, reason)
	})
}

// checkBusiness checks the source can be debited and the destination can receive.
func checkBusiness(from, to domain.Wallet, amount decimal.Decimal) error {
	if err := from.CanDebit(amount); err != nil {
		return err
	}
	return to.CanReceive()
}

// isBusinessFailure reports whether an error is a business failure that should be
// saved as FAILED rather than retried.
func isBusinessFailure(err error) bool {
	return errors.Is(err, domain.ErrWalletNotFound) ||
		errors.Is(err, domain.ErrWalletInactive) ||
		errors.Is(err, domain.ErrInsufficientFunds)
}
