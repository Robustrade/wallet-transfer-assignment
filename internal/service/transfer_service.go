// Package service holds orchestration: hash computation, cache-check,
// transaction boundary, calling repositories and domain rules. This is
// where the whole idempotency/concurrency flow lives.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/logger"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository"
)

// TransferService orchestrates the full idempotency/concurrency/ledger
// flow for wallet-to-wallet transfers.
type TransferService struct {
	db           repository.Transactor
	walletRepo   WalletRepository
	transferRepo TransferRepository
	cache        Cache
	idGen        IDGenerator
	lockTimeout  time.Duration
	logger       *slog.Logger
}

// NewTransferService constructs a TransferService.
func NewTransferService(
	db repository.Transactor,
	walletRepo WalletRepository,
	transferRepo TransferRepository,
	cache Cache,
	idGen IDGenerator,
	lockTimeout time.Duration,
	logger *slog.Logger,
) *TransferService {
	return &TransferService{
		db:           db,
		walletRepo:   walletRepo,
		transferRepo: transferRepo,
		cache:        cache,
		idGen:        idGen,
		lockTimeout:  lockTimeout,
		logger:       logger,
	}
}

// InitTransfer mints a fresh transfer ID. Nothing is persisted.
//
// A NextID failure (e.g. a Snowflake clock regression) is an
// infrastructure problem, not a client mistake — the real cause is
// logged here, server-side, and the caller gets back a generic typed
// error rather than the underlying detail.
func (s *TransferService) InitTransfer() (int64, error) {
	id, err := s.idGen.NextID()
	if err != nil {
		s.logger.Error("mint transfer id failed", slog.Any("error", err))
		return 0, domain.ErrTransferInitFailed
	}
	return id, nil
}

// Transfer runs, in order: same-wallet/invalid-amount checks, the cache
// check, a pre-transaction check for an already-committed id, and — only
// if truly new — locking wallets and claiming the transfer.
func (s *TransferService) Transfer(ctx context.Context, req TransferRequest) (*domain.Transfer, error) {
	if err := validateTransferRules(req); err != nil {
		return nil, err
	}

	hash := computeHash(req.FromWalletID, req.ToWalletID, req.AmountMinorUnits)

	cached, err := s.checkCache(ctx, req.ID, hash)
	if err != nil || cached != nil {
		return cached, err
	}

	existing, err := s.checkExistingTransfer(ctx, req.ID, hash)
	if err != nil || existing != nil {
		return existing, err
	}

	return s.lockAndExecuteTransfer(ctx, req, hash)
}

// GetTransfer reads a transfer by ID with no lock. It returns (nil, nil)
// if no such transfer exists.
func (s *TransferService) GetTransfer(ctx context.Context, id int64) (*domain.Transfer, error) {
	return s.transferRepo.FindByID(ctx, id)
}

// validateTransferRules checks same-wallet and invalid-amount. It
// touches neither Redis nor Postgres, and its outcome depends only on
// the request body — so a corrected resubmission can freely reuse the
// same id, since nothing was ever claimed against it. Kept in the
// service layer, not the handler: this is request *meaning*, not
// *shape*, and business rules don't belong in handlers.
func validateTransferRules(req TransferRequest) error {
	if req.FromWalletID == req.ToWalletID {
		return &domain.Error{Code: domain.ErrCodeSameWallet, Message: "fromWalletId and toWalletId must differ"}
	}
	if req.AmountMinorUnits <= 0 {
		return &domain.Error{Code: domain.ErrCodeInvalidAmount, Message: "amount must be greater than zero"}
	}
	return nil
}

// computeHash canonicalizes the parts of a transfer request that must
// match on replay. It deliberately excludes the transfer ID itself.
func computeHash(fromWalletID, toWalletID string, amountMinorUnits int64) string {
	canonical := fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amountMinorUnits)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// checkCache is the Redis half of the flow. A nil, nil result means a
// cache miss — the caller should fall through to the Postgres checks.
func (s *TransferService) checkCache(ctx context.Context, id int64, hash string) (*domain.Transfer, error) {
	cached, err := s.cache.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("check transfer cache: %w", err)
	}
	if cached == nil {
		return nil, nil
	}
	if cached.RequestHash != hash {
		return nil, idempotencyMismatchError()
	}
	return cached, nil
}

// checkExistingTransfer reads a transfer by ID with no lock and, if
// found, resolves it against hash: a match replays it, a mismatch is a
// 409. A nil, nil result means no such transfer exists (yet). Used both
// before the transaction starts (the common case: an already-committed
// id never touches wallet locks) and after losing the claim race in
// lockAndExecuteTransfer (the rare true-concurrent case) — checking here
// first means a reused id with a mismatched hash always returns 409,
// never a stale WALLET_NOT_FOUND from re-validating against wallets that
// may since look different.
func (s *TransferService) checkExistingTransfer(ctx context.Context, id int64, hash string) (*domain.Transfer, error) {
	existing, err := s.transferRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("check existing transfer: %w", err)
	}
	if existing == nil {
		return nil, nil
	}

	s.writeCache(ctx, existing)

	if existing.RequestHash != hash {
		return nil, idempotencyMismatchError()
	}
	return existing, nil
}

// lockAndExecuteTransfer is the Postgres half of the flow: BEGIN, lock
// both wallets first (the only place any lock is taken), decide and
// apply the outcome, then claim the transfer ID with that outcome
// already written — resolving a lost race against the already-committed
// winner if we lose it.
//
// Wallets lock before the claim insert, not after: the claim INSERT
// references from/to wallet FKs, and Postgres takes an implicit shared
// FOR KEY SHARE lock on both referenced rows as part of that insert. If
// the claim ran first, N concurrent transfers on the same wallet pair
// would all hold that shared lock simultaneously, then all try to
// upgrade to the explicit FOR UPDATE lock at once — a lock-upgrade
// deadlock, confirmed empirically under the concurrency stress test.
// Locking wallets first means only one transaction can ever be in that
// danger zone at a time.
func (s *TransferService) lockAndExecuteTransfer(ctx context.Context, req TransferRequest, hash string) (*domain.Transfer, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := setLockTimeout(ctx, tx, s.lockTimeout); err != nil {
		return nil, err
	}

	wallets, err := s.lockAndValidateWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return nil, err
	}

	status, failureReason, err := s.executeTransfer(ctx, tx, req, wallets)
	if err != nil {
		return nil, err
	}

	claimed, err := s.transferRepo.InsertTransfer(ctx, tx, req.ID, hash, req.FromWalletID, req.ToWalletID, req.Amount, status, failureReason)
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		_ = tx.Rollback(ctx)
		return s.checkConflictAfterRace(ctx, req.ID, hash)
	}

	if status == domain.StatusProcessed {
		if err := s.transferRepo.InsertLedgerEntries(ctx, tx, req.ID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transfer transaction: %w", err)
	}

	s.writeCache(ctx, claimed)
	return claimed, nil
}

// checkConflictAfterRace handles the rare case where a genuinely
// concurrent request for the same new id won the ON CONFLICT race first
// — by the time our INSERT observed the conflict, the winner has
// already committed (Postgres's speculative-insertion protocol only
// resolves the conflict after that), so the existing row is guaranteed
// visible here.
func (s *TransferService) checkConflictAfterRace(ctx context.Context, id int64, hash string) (*domain.Transfer, error) {
	existing, err := s.checkExistingTransfer(ctx, id, hash)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("transfer %d: lost the claim race but no row found", id)
	}
	return existing, nil
}

// lockAndValidateWallets locks both wallets — GetForUpdate sorts
// walletIDs internally before acquiring locks, so every caller ends up
// requesting the same pair in the same order regardless of the order
// passed here, which can't deadlock — and reports WALLET_NOT_FOUND if
// either is missing.
func (s *TransferService) lockAndValidateWallets(ctx context.Context, tx repository.Tx, fromID, toID string) (map[string]domain.Wallet, error) {
	wallets, err := s.walletRepo.GetForUpdate(ctx, tx, fromID, toID)
	if err != nil {
		return nil, fmt.Errorf("lock wallets: %w", err)
	}
	if len(wallets) < 2 {
		return nil, &domain.Error{Code: domain.ErrCodeWalletNotFound, Message: "fromWalletId or toWalletId does not exist"}
	}

	byID := make(map[string]domain.Wallet, len(wallets))
	for _, w := range wallets {
		byID[w.ID] = w
	}
	return byID, nil
}

// executeTransfer decides whether the source wallet has sufficient
// balance. Insufficient balance debits nothing and returns a
// FAILED/INSUFFICIENT_BALANCE outcome for the caller to persist as-is;
// sufficient balance debits and credits both wallets and returns
// PROCESSED. Insufficient balance is persisted (unlike same-wallet or
// invalid-amount) because it depends on mutable state — a wallet's
// balance can change between one delivery of a request and a later one,
// so the answer must be pinned to whatever it was the first time,
// otherwise the same idempotency key could yield different results.
func (s *TransferService) executeTransfer(ctx context.Context, tx repository.Tx, req TransferRequest, wallets map[string]domain.Wallet) (domain.Status, *domain.ErrorCode, error) {
	from := wallets[req.FromWalletID]
	to := wallets[req.ToWalletID]

	if from.Balance.LessThan(req.Amount) {
		reason := domain.ErrCodeInsufficientBalance
		return domain.StatusFailed, &reason, nil
	}

	if err := s.walletRepo.UpdateBalance(ctx, tx, from.ID, from.Balance.Sub(req.Amount)); err != nil {
		return "", nil, err
	}
	if err := s.walletRepo.UpdateBalance(ctx, tx, to.ID, to.Balance.Add(req.Amount)); err != nil {
		return "", nil, err
	}
	return domain.StatusProcessed, nil, nil
}

// writeCache best-effort caches transfer. Cache is a latency optimization
// a failure here must never fail a request that already
// committed successfully in Postgres.
func (s *TransferService) writeCache(ctx context.Context, transfer *domain.Transfer) {
	if err := s.cache.Set(ctx, transfer.ID, transfer); err != nil {
		logger.FromContext(ctx, s.logger).Warn("cache transfer failed", slog.Any("error", err))
	}
}

// setLockTimeout requires timeout to already be at least 1ms —
// config.Validate() enforces that at startup. This clamp is a second,
// defensive layer only, for any path that builds a TransferService
// without going through config validation (e.g. a test): under 1ms,
// timeout.Milliseconds() truncates to 0, and "SET LOCAL lock_timeout =
// '0ms'" means "no timeout" in Postgres — the opposite of what a
// lock_timeout is for.
func setLockTimeout(ctx context.Context, tx repository.Tx, timeout time.Duration) error {
	if timeout < time.Millisecond {
		timeout = time.Millisecond
	}
	_, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = '%dms'", timeout.Milliseconds()))
	if err != nil {
		return fmt.Errorf("set lock_timeout: %w", err)
	}
	return nil
}

func idempotencyMismatchError() error {
	return &domain.Error{
		Code:    domain.ErrCodeIdempotencyKeyReused,
		Message: "idempotencyKey was already used with a different fromWalletId, toWalletId, or amount",
	}
}
