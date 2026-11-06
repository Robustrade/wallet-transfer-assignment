package service

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// TransferService executes wallet-to-wallet transfers with exactly-once
// semantics per idempotency key. See docs/DESIGN.md §4–§6. It contains only the
// workflow; cross-cutting concerns such as logging are decorators (logging.go).
type TransferService struct {
	db    Database
	now   func() time.Time
	newID func() uuid.UUID
}

// NewTransferService returns a TransferService backed by db.
func NewTransferService(db Database) *TransferService {
	return &TransferService{
		db: db,
		// Truncate to Postgres timestamp precision (µs): otherwise an original response
		// (ns clock on Linux) and its replay (read back from the DB) would differ.
		now:   func() time.Time { return time.Now().UTC().Truncate(time.Microsecond) },
		newID: uuid.New,
	}
}

// CreateTransferResult is the outcome of CreateTransfer.
type CreateTransferResult struct {
	// Transfer is PROCESSED or FAILED; never PENDING.
	Transfer domain.Transfer
	// Replayed is true when the result was produced by an earlier request with the same key.
	Replayed bool
}

// CreateTransfer executes req at most once per idempotency key.
//
// It returns a PROCESSED transfer, or a FAILED one when funds are insufficient;
// both outcomes are durable and replayed for the same key. Errors (validation,
// unknown wallet, key reuse, busy, infrastructure) persist nothing.
func (s *TransferService) CreateTransfer(ctx context.Context, req domain.TransferRequest) (CreateTransferResult, error) {
	if err := req.Validate(); err != nil {
		return CreateTransferResult{}, err
	}

	var result CreateTransferResult
	err := s.db.InTx(ctx, func(ctx context.Context, tx Store) error {
		var err error
		result, err = s.createInTx(ctx, tx, req)
		return err
	})
	if err != nil {
		return CreateTransferResult{}, err
	}
	return result, nil
}

// createInTx is the transactional body of CreateTransfer. Order matters:
//  1. claim the key first, so concurrent duplicates serialise before any other lock
//  2. lock both wallets in a fixed order, so opposite transfers cannot deadlock;
//     this must precede inserting the transfer, whose FK checks take KEY SHARE locks
//     on the wallet rows that would otherwise conflict with a later FOR UPDATE
//  3. insert the transfer, apply the domain decision, write balances and ledger
func (s *TransferService) createInTx(ctx context.Context, tx Store, req domain.TransferRequest) (CreateTransferResult, error) {
	transfer, err := domain.NewPendingTransfer(s.newID(), req, s.now())
	if err != nil {
		return CreateTransferResult{}, err
	}

	claimed, prior, err := claimKey(ctx, tx, req, transfer.ID)
	if err != nil || !claimed {
		return prior, err // replay of the original outcome, or a failure
	}

	source, err := lockWallets(ctx, tx.Wallets(), req.FromWalletID, req.ToWalletID)
	if err != nil {
		return CreateTransferResult{}, err
	}

	if err := tx.Transfers().Create(ctx, transfer); err != nil {
		return CreateTransferResult{}, err
	}
	if err := transfer.Execute(source, s.now()); err != nil {
		return CreateTransferResult{}, err
	}
	if transfer.Status == domain.StatusProcessed {
		if err := s.moveMoney(ctx, tx, transfer); err != nil {
			return CreateTransferResult{}, err
		}
	}
	if err := tx.Transfers().UpdateStatus(ctx, transfer, domain.StatusPending); err != nil {
		return CreateTransferResult{}, err
	}
	return CreateTransferResult{Transfer: transfer}, nil
}

// moveMoney applies a processed transfer's balance changes and its ledger pair.
func (s *TransferService) moveMoney(ctx context.Context, tx Store, t domain.Transfer) error {
	entries, err := t.LedgerEntries()
	if err != nil {
		return err
	}
	if err := tx.Wallets().Debit(ctx, t.FromWalletID, t.Amount); err != nil {
		return err
	}
	if err := tx.Wallets().Credit(ctx, t.ToWalletID, t.Amount); err != nil {
		return err
	}
	return tx.Ledger().Append(ctx, entries)
}

// lockWallets row-locks both wallets in ascending id order and returns the source wallet.
func lockWallets(ctx context.Context, wallets WalletRepository, fromID, toID string) (domain.Wallet, error) {
	ids := []string{fromID, toID}
	sort.Strings(ids)

	var source domain.Wallet
	for _, id := range ids {
		w, err := wallets.GetForUpdate(ctx, id)
		if err != nil {
			return domain.Wallet{}, err
		}
		if id == fromID {
			source = w
		}
	}
	return source, nil
}
