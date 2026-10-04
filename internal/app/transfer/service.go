package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/app/port"
	"github.com/amantiwari/wallet-transfer/internal/domain"
	"github.com/amantiwari/wallet-transfer/internal/platform/requestid"
)

// Command is a request to move amount from one wallet to another.
type Command struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Result is the terminal transfer produced or replayed by Execute.
type Result struct {
	Transfer domain.Transfer
	Replayed bool
}

// Service executes wallet transfers.
type Service struct {
	uow       port.UnitOfWork
	transfers port.TransferReader
	ledger    port.LedgerReader
	clock     port.Clock
	ids       port.IDGenerator
	log       *slog.Logger
}

// NewService constructs the transfer use case.
func NewService(
	uow port.UnitOfWork,
	transfers port.TransferReader,
	ledger port.LedgerReader,
	clock port.Clock,
	ids port.IDGenerator,
	log *slog.Logger,
) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		uow:       uow,
		transfers: transfers,
		ledger:    ledger,
		clock:     clock,
		ids:       ids,
		log:       log,
	}
}

// Execute moves funds once for an idempotency key.
// A repeated key with the same wallets and amount returns the original transfer
// and does not post again. A repeated key with a different payload is rejected.
func (s *Service) Execute(ctx context.Context, cmd Command) (Result, error) {
	if err := validateCommand(cmd); err != nil {
		return Result{}, err
	}

	var result Result
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos port.Repositories) error {
		outcome, err := s.executeTx(ctx, repos, cmd)
		if err != nil {
			return err
		}
		result = outcome
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	s.log.InfoContext(ctx, "transfer result",
		"request_id", requestid.From(ctx),
		"transfer_id", result.Transfer.ID,
		"idempotency_key", result.Transfer.IdempotencyKey,
		"status", result.Transfer.Status,
		"failure_reason", result.Transfer.FailureReason,
		"replayed", result.Replayed,
		"amount", result.Transfer.Amount,
	)
	return result, nil
}

// Get returns a transfer by id.
func (s *Service) Get(ctx context.Context, id string) (domain.Transfer, error) {
	if id == "" {
		return domain.Transfer{}, &domain.ValidationError{Details: []domain.FieldError{{
			Field:   "transferId",
			Message: "is required",
		}}}
	}
	transfer, err := s.transfers.GetByID(ctx, id)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("get transfer: %w", err)
	}
	return transfer, nil
}

// Ledger returns the double-entry rows for a transfer.
// A failed transfer has an empty slice. A missing transfer is an error.
func (s *Service) Ledger(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	if _, err := s.Get(ctx, transferID); err != nil {
		return nil, err
	}
	entries, err := s.ledger.ListByTransferID(ctx, transferID)
	if err != nil {
		return nil, fmt.Errorf("list ledger: %w", err)
	}
	return entries, nil
}

func (s *Service) executeTx(ctx context.Context, repos port.Repositories, cmd Command) (Result, error) {
	now := s.clock.Now().UTC()
	hash := fingerprint(cmd)
	record, inserted, err := repos.Idempotency().TryInsert(ctx, port.IdempotencyRecord{
		Key:         cmd.IdempotencyKey,
		RequestHash: hash,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		return Result{}, fmt.Errorf("claim idempotency key: %w", err)
	}
	if !inserted {
		return s.replay(ctx, repos, hash, record)
	}

	fromWallet, toWallet, err := lockParties(ctx, repos.Wallets(), cmd.FromWalletID, cmd.ToWalletID)
	if err != nil {
		return Result{}, err
	}
	if toWallet.Balance > math.MaxInt64-cmd.Amount {
		return Result{}, domain.ErrAmountOverflow
	}

	pending, err := domain.NewPendingTransfer(s.ids.NewID(), cmd.IdempotencyKey, cmd.FromWalletID, cmd.ToWalletID, cmd.Amount, now)
	if err != nil {
		return Result{}, err
	}
	if fromWallet.Balance < cmd.Amount {
		return s.failInsufficient(ctx, repos, pending, now)
	}
	return s.settle(ctx, repos, pending, now)
}

func (s *Service) replay(ctx context.Context, repos port.Repositories, hash string, record port.IdempotencyRecord) (Result, error) {
	if record.RequestHash != hash {
		return Result{}, domain.ErrIdempotencyMismatch
	}
	if record.TransferID == "" {
		return Result{}, fmt.Errorf("idempotency key %s has no transfer: %w", record.Key, domain.ErrInvariant)
	}
	transfer, err := repos.Transfers().GetByID(ctx, record.TransferID)
	if err != nil {
		return Result{}, fmt.Errorf("load original transfer: %w", err)
	}
	return Result{Transfer: transfer, Replayed: true}, nil
}

func (s *Service) failInsufficient(ctx context.Context, repos port.Repositories, pending domain.Transfer, now time.Time) (Result, error) {
	if err := repos.Transfers().Insert(ctx, pending); err != nil {
		return Result{}, fmt.Errorf("insert transfer: %w", err)
	}
	failed, err := pending.MarkFailed(domain.ReasonInsufficientFunds, now)
	if err != nil {
		return Result{}, err
	}
	if err := repos.Transfers().Transition(ctx, failed, domain.StatusPending); err != nil {
		return Result{}, fmt.Errorf("fail transfer: %w", err)
	}
	if err := repos.Idempotency().AttachTransfer(ctx, pending.IdempotencyKey, pending.ID, now); err != nil {
		return Result{}, fmt.Errorf("attach idempotency key: %w", err)
	}
	return Result{Transfer: failed}, nil
}

func (s *Service) settle(ctx context.Context, repos port.Repositories, pending domain.Transfer, now time.Time) (Result, error) {
	if err := repos.Transfers().Insert(ctx, pending); err != nil {
		return Result{}, fmt.Errorf("insert transfer: %w", err)
	}
	if err := repos.Wallets().Debit(ctx, pending.FromWalletID, pending.Amount, now); err != nil {
		return Result{}, fmt.Errorf("debit source wallet: %w", err)
	}
	if err := repos.Wallets().Credit(ctx, pending.ToWalletID, pending.Amount, now); err != nil {
		return Result{}, fmt.Errorf("credit destination wallet: %w", err)
	}
	debit, credit, err := domain.LedgerPair(pending, s.ids.NewID(), s.ids.NewID(), now)
	if err != nil {
		return Result{}, err
	}
	if err := repos.Ledger().InsertPair(ctx, debit, credit); err != nil {
		return Result{}, fmt.Errorf("insert ledger: %w", err)
	}
	processed, err := pending.MarkProcessed(now)
	if err != nil {
		return Result{}, err
	}
	if err := repos.Transfers().Transition(ctx, processed, domain.StatusPending); err != nil {
		return Result{}, fmt.Errorf("process transfer: %w", err)
	}
	if err := repos.Idempotency().AttachTransfer(ctx, pending.IdempotencyKey, pending.ID, now); err != nil {
		return Result{}, fmt.Errorf("attach idempotency key: %w", err)
	}
	return Result{Transfer: processed}, nil
}

func validateCommand(cmd Command) error {
	details := make([]domain.FieldError, 0, 4)
	if err := domain.ValidateIdempotencyKey(cmd.IdempotencyKey); err != nil {
		details = append(details, fieldErrors(err, "idempotencyKey")...)
	}
	if err := domain.ValidateWalletID(cmd.FromWalletID); err != nil {
		details = append(details, fieldErrors(err, "fromWalletId")...)
	}
	if err := domain.ValidateWalletID(cmd.ToWalletID); err != nil {
		details = append(details, fieldErrors(err, "toWalletId")...)
	}
	if cmd.FromWalletID != "" && cmd.FromWalletID == cmd.ToWalletID {
		details = append(details, domain.FieldError{
			Field:   "toWalletId",
			Message: "must be different from fromWalletId",
		})
	}
	if err := domain.ValidateTransferAmount(cmd.Amount); err != nil {
		details = append(details, fieldErrors(err, "")...)
	}
	if len(details) > 0 {
		return &domain.ValidationError{Details: details}
	}
	return nil
}

func fieldErrors(err error, field string) []domain.FieldError {
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		name := field
		if name == "" {
			name = "request"
		}
		return []domain.FieldError{{Field: name, Message: err.Error()}}
	}
	if field == "" {
		out := make([]domain.FieldError, len(validation.Details))
		copy(out, validation.Details)
		return out
	}
	out := make([]domain.FieldError, len(validation.Details))
	for i, detail := range validation.Details {
		detail.Field = field
		out[i] = detail
	}
	return out
}

func lockParties(ctx context.Context, wallets port.WalletRepository, fromID, toID string) (domain.Wallet, domain.Wallet, error) {
	ids := []string{fromID, toID}
	sort.Strings(ids)
	locked := make(map[string]domain.Wallet, len(ids))
	for _, id := range ids {
		if _, seen := locked[id]; seen {
			continue
		}
		wallet, err := wallets.Lock(ctx, id)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				return domain.Wallet{}, domain.Wallet{}, err
			}
			return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("lock wallet %s: %w", id, err)
		}
		locked[id] = wallet
	}
	return locked[fromID], locked[toID], nil
}

func fingerprint(cmd Command) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", cmd.FromWalletID, cmd.ToWalletID, cmd.Amount)))
	return hex.EncodeToString(sum[:])
}
