package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

// TransferService provides transactional wallet transfer operations.
type TransferService struct {
	txManager         repository.TxManager
	repos             repository.Repositories
	heartbeatRepo     repository.IdempotencyRepository
	staleTimeout      time.Duration
	heartbeatInterval time.Duration
	txSem             chan struct{}
}

// NewTransferService constructs a TransferService with default transaction capacity (20).
func NewTransferService(txManager repository.TxManager, repos repository.Repositories) *TransferService {
	return NewTransferServiceWithCapacity(txManager, repos, 20)
}

// NewTransferServiceWithCapacity constructs a TransferService with explicit transaction concurrency capacity,
// reserving the remaining connection pool headroom for background lease heartbeats and non-tx queries.
func NewTransferServiceWithCapacity(txManager repository.TxManager, repos repository.Repositories, maxConcurrentTransfers int) *TransferService {
	if maxConcurrentTransfers <= 0 {
		maxConcurrentTransfers = 20
	}
	return &TransferService{
		txManager:         txManager,
		repos:             repos,
		staleTimeout:      30 * time.Second,
		heartbeatInterval: 5 * time.Second,
		txSem:             make(chan struct{}, maxConcurrentTransfers),
	}
}

// SetHeartbeatRepo configures a dedicated repository for background lease heartbeats (e.g. backed by a dedicated connection pool).
func (s *TransferService) SetHeartbeatRepo(repo repository.IdempotencyRepository) {
	s.heartbeatRepo = repo
}

// SetStaleTimeout overrides the default stale reservation timeout (useful for testing).
func (s *TransferService) SetStaleTimeout(d time.Duration) {
	s.staleTimeout = d
}

// SetHeartbeatInterval overrides the default heartbeat interval (useful for testing).
func (s *TransferService) SetHeartbeatInterval(d time.Duration) {
	s.heartbeatInterval = d
}

// ExecuteTransfer orchestrates a safe, idempotent, double-entry wallet transfer.
func (s *TransferService) ExecuteTransfer(ctx context.Context, req CreateTransferRequest) (*TransferResponse, error) {
	// 1. Validate the idempotency key itself first
	if req.IdempotencyKey == "" {
		return nil, domain.ErrMissingIdempotencyKey
	}
	if len(req.IdempotencyKey) > 128 {
		return nil, domain.ErrInvalidIdempotencyKey
	}

	requestHash := domain.ComputeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)

	// 2. Resolve existing idempotency key against canonical request fingerprint before business-field validation.
	// This ensures that reusing an existing key with changed but invalid parameters (e.g. negative amount,
	// missing wallet ID, or identical source/destination) returns 409 Conflict instead of 400/404.
	existing, err := s.repos.Idempotency.GetIdempotency(ctx, req.IdempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("error resolving idempotency record: %w", err)
	}
	if existing != nil {
		if existing.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyConflict
		}
		if existing.Status == domain.IdempotencyStatusCompleted || existing.Status == domain.IdempotencyStatusFailed {
			var cachedResp TransferResponse
			if jsonErr := json.Unmarshal([]byte(existing.ResponseBody), &cachedResp); jsonErr == nil {
				cachedResp.IsReplay = true
				cachedResp.ResponseCode = existing.ResponseCode
				if existing.Status == domain.IdempotencyStatusFailed {
					return &cachedResp, s.mapFailureReasonToError(cachedResp.FailureReason, existing.ResponseCode)
				}
				return &cachedResp, nil
			}
		}
		if existing.Status == domain.IdempotencyStatusInProgress {
			staleTimeout := s.staleTimeout
			if staleTimeout <= 0 {
				staleTimeout = 30 * time.Second
			}
			if time.Since(existing.UpdatedAt) <= staleTimeout {
				return nil, domain.ErrIdempotencyInProgress
			}
			// If stale, check if an underlying transfer was already committed for this idempotency key.
			// If a previous execution committed the transfer but crashed or failed to finalize idempotency_records,
			// recover and return the committed result rather than executing a duplicate transfer!
			existingTx, tErr := s.repos.Transfers.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
			if tErr != nil {
				if !errors.Is(tErr, domain.ErrTransferNotFound) && !errors.Is(tErr, pgx.ErrNoRows) {
					return nil, fmt.Errorf("failed to lookup transfer during stale recovery: %w", tErr)
				}
				// Explicit not-found: safe to fall through to business validation and atomic reclaim
			} else if existingTx != nil {
				switch existingTx.Status {
				case domain.TransferStatusProcessed:
					respCode := http.StatusCreated
					status := domain.IdempotencyStatusCompleted
					resp := &TransferResponse{
						TransferID:     existingTx.ID,
						IdempotencyKey: existingTx.IdempotencyKey,
						FromWalletID:   existingTx.FromWalletID,
						ToWalletID:     existingTx.ToWalletID,
						Amount:         existingTx.Amount,
						Status:         existingTx.Status,
						FailureReason:  nil,
						CreatedAt:      existingTx.CreatedAt,
						IsReplay:       true,
						ResponseCode:   respCode,
					}
					respBytes, _ := json.Marshal(resp)
					existing.Status = status
					existing.TransferID = &existingTx.ID
					existing.ResponseCode = respCode
					existing.ResponseBody = string(respBytes)
					if updateErr := s.repos.Idempotency.UpdateIdempotency(ctx, existing); updateErr != nil {
						return nil, fmt.Errorf("failed to finalize recovered idempotency record: %w", updateErr)
					}
					return resp, nil

				case domain.TransferStatusFailed:
					respCode := http.StatusUnprocessableEntity
					status := domain.IdempotencyStatusFailed
					failureReason := existingTx.FailureReason
					resp := &TransferResponse{
						TransferID:     existingTx.ID,
						IdempotencyKey: existingTx.IdempotencyKey,
						FromWalletID:   existingTx.FromWalletID,
						ToWalletID:     existingTx.ToWalletID,
						Amount:         existingTx.Amount,
						Status:         existingTx.Status,
						FailureReason:  failureReason,
						CreatedAt:      existingTx.CreatedAt,
						IsReplay:       true,
						ResponseCode:   respCode,
					}
					respBytes, _ := json.Marshal(resp)
					existing.Status = status
					existing.TransferID = &existingTx.ID
					existing.ResponseCode = respCode
					existing.ResponseBody = string(respBytes)
					if updateErr := s.repos.Idempotency.UpdateIdempotency(ctx, existing); updateErr != nil {
						return nil, fmt.Errorf("failed to finalize recovered idempotency record: %w", updateErr)
					}
					return resp, s.mapFailureReasonToError(failureReason, respCode)

				default:
					// Transfer is still in non-terminal PENDING state; do not treat as COMPLETED and do not allow reclaim.
					// Retain the in-progress reservation and return ErrIdempotencyInProgress.
					return nil, domain.ErrIdempotencyInProgress
				}
			}
			// If stale and no transfer was committed, fall through to business validation and atomic reclaim in ReserveIdempotency.
		}
	}

	// Helper to recheck the idempotency key if preflight validation fails.
	// A concurrent request might have reserved or completed the key in the gap between GetIdempotency
	// and preflight validation. Rechecking guarantees concurrent reuses receive 409 Conflict/In-Progress
	// with the exact same precedence as sequential reuses.
	cleanupStaleReservation := func() {
		if existing != nil && existing.Status == domain.IdempotencyStatusInProgress {
			// Do not delete reservation if a transfer was already committed or if lookup encountered an operational error!
			existingTx, tErr := s.repos.Transfers.GetTransferByIdempotencyKey(ctx, req.IdempotencyKey)
			if tErr != nil {
				// Operational error: retain the reservation
				if !errors.Is(tErr, domain.ErrTransferNotFound) && !errors.Is(tErr, pgx.ErrNoRows) {
					return
				}
			} else if existingTx != nil {
				// Transfer exists: retain the reservation
				return
			}
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.repos.Idempotency.DeleteStaleInProgress(cleanupCtx, req.IdempotencyKey, existing.OwnerToken, existing.UpdatedAt)
		}
	}

	checkValidationErr := func(validationErr error) error {
		cleanupStaleReservation()
		recheck, rErr := s.repos.Idempotency.GetIdempotency(ctx, req.IdempotencyKey)
		if rErr == nil && recheck != nil {
			if recheck.RequestHash != requestHash {
				return domain.ErrIdempotencyConflict
			}
			if recheck.Status == domain.IdempotencyStatusInProgress {
				staleTimeout := s.staleTimeout
				if staleTimeout <= 0 {
					staleTimeout = 30 * time.Second
				}
				if time.Since(recheck.UpdatedAt) <= staleTimeout {
					return domain.ErrIdempotencyInProgress
				}
			}
			// If already completed or failed, return conflict since this request failed validation
			return domain.ErrIdempotencyConflict
		}
		return validationErr
	}

	// 3. Business-field validation (for new keys or stale lock reclaims)
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return nil, checkValidationErr(domain.ErrMissingWalletID)
	}
	if req.FromWalletID == domain.SystemTreasuryWalletID || req.ToWalletID == domain.SystemTreasuryWalletID {
		return nil, checkValidationErr(domain.ErrSystemTreasuryRestricted)
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, checkValidationErr(domain.ErrSameWalletTransfer)
	}
	if req.Amount <= 0 {
		return nil, checkValidationErr(domain.ErrInvalidAmount)
	}

	// Validate wallet existence, currency compatibility, and overflow before reserving idempotency key.
	// This ensures invalid requests fail fast without reserving or polluting idempotency records.
	fromWalletPre, err := s.repos.Wallets.GetWalletByID(ctx, req.FromWalletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) || errors.Is(err, pgx.ErrNoRows) {
			return nil, checkValidationErr(domain.ErrWalletNotFound)
		}
		return nil, fmt.Errorf("failed to verify source wallet: %w", err)
	}
	toWalletPre, err := s.repos.Wallets.GetWalletByID(ctx, req.ToWalletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) || errors.Is(err, pgx.ErrNoRows) {
			return nil, checkValidationErr(domain.ErrWalletNotFound)
		}
		return nil, fmt.Errorf("failed to verify destination wallet: %w", err)
	}
	if fromWalletPre.Currency != toWalletPre.Currency {
		return nil, checkValidationErr(domain.ErrCurrencyMismatch)
	}

	// 4. Short committed reservation outside the transfer transaction.
	// This immediately inserts and commits an IN_PROGRESS reservation.
	// Overlapping concurrent requests immediately see IN_PROGRESS and return 409 Conflict,
	// without blocking on an uncommitted row or starving pool connections for the duration of the transfer.
	staleTimeout := s.staleTimeout
	if staleTimeout <= 0 {
		staleTimeout = 30 * time.Second
	}

	ownerToken := uuid.NewString()
	record := &domain.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    requestHash,
		OwnerToken:     ownerToken,
		Status:         domain.IdempotencyStatusInProgress,
	}

	reservedRecord, isOwner, err := s.repos.Idempotency.ReserveIdempotency(ctx, record, staleTimeout)
	if err != nil {
		// Attempt bounded cleanup with ownerToken in case the reservation was inserted before scan/connection failure
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = s.repos.Idempotency.DeleteInProgress(cleanupCtx, req.IdempotencyKey, ownerToken)
		cancel()
		return nil, fmt.Errorf("error reserving idempotency record: %w", err)
	}

	if !isOwner {
		// Key already exists (or another process is currently executing)
		if reservedRecord.RequestHash != requestHash {
			return nil, domain.ErrIdempotencyConflict
		}
		if reservedRecord.Status == domain.IdempotencyStatusCompleted || reservedRecord.Status == domain.IdempotencyStatusFailed {
			var cachedResp TransferResponse
			if jsonErr := json.Unmarshal([]byte(reservedRecord.ResponseBody), &cachedResp); jsonErr == nil {
				cachedResp.IsReplay = true
				cachedResp.ResponseCode = reservedRecord.ResponseCode
				if reservedRecord.Status == domain.IdempotencyStatusFailed {
					return &cachedResp, domain.ErrInsufficientFunds
				}
				return &cachedResp, nil
			}
		}
		if reservedRecord.Status == domain.IdempotencyStatusInProgress {
			return nil, domain.ErrIdempotencyInProgress
		}
		return nil, domain.ErrIdempotencyConflict
	}

	// Create execution context that can be cancelled immediately if the heartbeat discovers our lease was lost.
	txCtx, cancelTx := context.WithCancel(ctx)
	defer cancelTx()

	var leaseLostErr error
	var leaseMu sync.Mutex

	// Start background lease heartbeat to refresh updated_at while the transaction is actively executing.
	// This prevents a live but slow transaction from being reclaimed prematurely by stale-owner recovery.
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	var stopHeartbeatOnce sync.Once
	stopAndJoinHeartbeat := func() {
		stopHeartbeatOnce.Do(func() {
			close(stopHeartbeat)
			<-heartbeatDone
		})
	}
	defer stopAndJoinHeartbeat()

	go func() {
		defer close(heartbeatDone)
		interval := s.heartbeatInterval
		if interval <= 0 {
			interval = 5 * time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		lastHeartbeatSuccess := time.Now()
		consecutiveFailures := 0
		maxUnrefreshedDuration := staleTimeout / 2
		if maxUnrefreshedDuration < 2*interval {
			maxUnrefreshedDuration = 2 * interval
		}

		for {
			select {
			case <-stopHeartbeat:
				return
			case <-txCtx.Done():
				return
			case <-ticker.C:
				hbTimeout := interval / 2
				if hbTimeout < time.Second {
					hbTimeout = 3 * time.Second
				}
				hbCtx, hbCancel := context.WithTimeout(context.Background(), hbTimeout)
				hbRepo := s.heartbeatRepo
				if hbRepo == nil {
					hbRepo = s.repos.Idempotency
				}
				hbErr := hbRepo.HeartbeatIdempotency(hbCtx, req.IdempotencyKey, ownerToken)
				hbCancel()
				if hbErr != nil {
					// Discard heartbeat error if heartbeat was already signaled to stop (e.g. after commit)
					select {
					case <-stopHeartbeat:
						return
					default:
					}

					consecutiveFailures++

					if errors.Is(hbErr, domain.ErrIdempotencyLeaseLost) {
						leaseMu.Lock()
						leaseLostErr = domain.ErrIdempotencyLeaseLost
						leaseMu.Unlock()
						cancelTx() // Abort in-flight transaction immediately to release wallet locks
						return
					}

					// Track non-lease-loss heartbeat failures (e.g. DB connection dropped, timeout).
					// If refreshes cannot be completed reliably before the 30s stale window expires,
					// abort the transaction to prevent holding locks with an unrefreshed, reclaimable lease.
					if time.Since(lastHeartbeatSuccess) >= maxUnrefreshedDuration || consecutiveFailures >= 2 {
						leaseMu.Lock()
						leaseLostErr = fmt.Errorf("heartbeat lease refresh failed reliably: %w", hbErr)
						leaseMu.Unlock()
						cancelTx() // Abort in-flight transaction immediately
						return
					}
				} else {
					lastHeartbeatSuccess = time.Now()
					consecutiveFailures = 0
				}
			}
		}
	}()

	var transferID string
	var respBytes []byte
	var txCompleted bool
	var finalResponse *TransferResponse
	var isInsufficientFunds bool

	// The caller owns the reservation.
	// If the transfer execution aborts unexpectedly prior to commit (e.g. pre-commit failure, context cancellation, DB drop),
	// delete the in-progress reservation so subsequent retries do not have to wait for stale timeout.
	// However, if the transaction completed its operations and failed only at commit (ambiguous commit),
	// do NOT delete the reservation, as PostgreSQL may have committed the transfer.
	success := false
	defer func() {
		if !success && !txCompleted {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = s.repos.Idempotency.DeleteInProgress(cleanupCtx, req.IdempotencyKey, ownerToken)
		}
	}()

	// Acquire transaction semaphore slot to guarantee connection pool headroom for background heartbeats and reads.
	// Capturing sem locally guarantees that release evaluates the exact same channel.
	sem := s.txSem
	if sem != nil {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-txCtx.Done():
			return nil, txCtx.Err()
		}
	}

	// 5. Execute transfer inside transaction
	txErr := s.txManager.ExecuteInTx(txCtx, func(txRepos repository.Repositories) error {
		// Deterministic lock ordering to prevent deadlocks:
		// Always acquire locks in alphabetical order: min(fromID, toID) followed by max(fromID, toID)
		firstID, secondID := req.FromWalletID, req.ToWalletID
		if firstID > secondID {
			firstID, secondID = secondID, firstID
		}

		wallet1, lockErr1 := txRepos.Wallets.GetWalletByIDForUpdate(txCtx, firstID)
		if lockErr1 != nil {
			if errors.Is(lockErr1, domain.ErrWalletNotFound) || errors.Is(lockErr1, pgx.ErrNoRows) {
				return domain.ErrWalletNotFound
			}
			return lockErr1
		}
		wallet2, lockErr2 := txRepos.Wallets.GetWalletByIDForUpdate(txCtx, secondID)
		if lockErr2 != nil {
			if errors.Is(lockErr2, domain.ErrWalletNotFound) || errors.Is(lockErr2, pgx.ErrNoRows) {
				return domain.ErrWalletNotFound
			}
			return lockErr2
		}

		var fromWallet, toWallet *domain.Wallet
		if wallet1.ID == req.FromWalletID {
			fromWallet, toWallet = wallet1, wallet2
		} else {
			fromWallet, toWallet = wallet2, wallet1
		}

		// Reject transfers between wallets with mismatched currencies
		if fromWallet.Currency != toWallet.Currency {
			return domain.ErrCurrencyMismatch
		}

		// Reject transfers where crediting destination would overflow MaxInt64
		if !toWallet.CanCredit(req.Amount) {
			return domain.ErrBalanceOverflow
		}

		// Initialize transfer record with PENDING state
		transferID = uuid.NewString()
		transfer := &domain.Transfer{
			ID:             transferID,
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   fromWallet.ID,
			ToWalletID:     toWallet.ID,
			Amount:         req.Amount,
			Status:         domain.TransferStatusPending,
		}
		if createErr := txRepos.Transfers.CreateTransfer(txCtx, transfer); createErr != nil {
			return createErr
		}

		// Check balance for debit
		if !fromWallet.CanDebit(req.Amount) {
			reason := "insufficient funds in source wallet"
			if markErr := transfer.MarkFailed(reason); markErr != nil {
				return markErr
			}
			if updateErr := txRepos.Transfers.UpdateTransferStatus(txCtx, transfer.ID, domain.TransferStatusFailed, &reason); updateErr != nil {
				return updateErr
			}

			resp := &TransferResponse{
				TransferID:     transfer.ID,
				IdempotencyKey: transfer.IdempotencyKey,
				FromWalletID:   transfer.FromWalletID,
				ToWalletID:     transfer.ToWalletID,
				Amount:         transfer.Amount,
				Status:         domain.TransferStatusFailed,
				FailureReason:  &reason,
				CreatedAt:      transfer.CreatedAt,
				ResponseCode:   http.StatusUnprocessableEntity,
			}
			var jsonErr error
			respBytes, jsonErr = json.Marshal(resp)
			if jsonErr != nil {
				return fmt.Errorf("failed to marshal failure response: %w", jsonErr)
			}

			record.Status = domain.IdempotencyStatusFailed
			record.TransferID = &transfer.ID
			record.ResponseCode = http.StatusUnprocessableEntity
			record.ResponseBody = string(respBytes)
			record.OwnerToken = ownerToken
			if updateErr := txRepos.Idempotency.UpdateIdempotency(txCtx, record); updateErr != nil {
				return updateErr
			}

			finalResponse = resp
			isInsufficientFunds = true
			txCompleted = true
			// Return nil so the transaction commits the FAILED transfer record and idempotency record
			return nil
		}

		// Sufficient balance: update balances
		if debitErr := fromWallet.Debit(req.Amount); debitErr != nil {
			return debitErr
		}
		if creditErr := toWallet.Credit(req.Amount); creditErr != nil {
			return creditErr
		}

		if updateErr := txRepos.Wallets.UpdateWalletBalance(txCtx, fromWallet.ID, fromWallet.Balance); updateErr != nil {
			return updateErr
		}
		if updateErr := txRepos.Wallets.UpdateWalletBalance(txCtx, toWallet.ID, toWallet.Balance); updateErr != nil {
			return updateErr
		}

		// Create balanced double-entry ledger records
		debitEntry, creditEntry, ledgerErr := domain.NewDoubleEntryPair(transfer.ID, fromWallet.ID, toWallet.ID, req.Amount)
		if ledgerErr != nil {
			return ledgerErr
		}
		if err := txRepos.Ledger.CreateLedgerEntries(txCtx, debitEntry, creditEntry); err != nil {
			return err
		}

		// Transition transfer to PROCESSED
		if markErr := transfer.MarkProcessed(); markErr != nil {
			return markErr
		}
		if err := txRepos.Transfers.UpdateTransferStatus(txCtx, transfer.ID, domain.TransferStatusProcessed, nil); err != nil {
			return err
		}

		// Update idempotency record to COMPLETED with cached response body
		resp := &TransferResponse{
			TransferID:     transfer.ID,
			IdempotencyKey: transfer.IdempotencyKey,
			FromWalletID:   transfer.FromWalletID,
			ToWalletID:     transfer.ToWalletID,
			Amount:         transfer.Amount,
			Status:         domain.TransferStatusProcessed,
			CreatedAt:      transfer.CreatedAt,
			ResponseCode:   http.StatusCreated,
		}
		var jsonErr error
		respBytes, jsonErr = json.Marshal(resp)
		if jsonErr != nil {
			return fmt.Errorf("failed to marshal transfer response: %w", jsonErr)
		}

		record.Status = domain.IdempotencyStatusCompleted
		record.TransferID = &transfer.ID
		record.ResponseCode = http.StatusCreated
		record.ResponseBody = string(respBytes)
		record.OwnerToken = ownerToken
		if updateErr := txRepos.Idempotency.UpdateIdempotency(txCtx, record); updateErr != nil {
			return updateErr
		}

		finalResponse = resp
		txCompleted = true
		return nil
	})

	// Stop and join the heartbeat before inspecting lease status or deciding the result.
	stopAndJoinHeartbeat()

	// If transaction successfully committed, treat successful commit as authoritative!
	if txErr == nil {
		success = true
		if isInsufficientFunds {
			return finalResponse, domain.ErrInsufficientFunds
		}
		return finalResponse, nil
	}

	// If transaction failed, check if it was aborted due to lease loss or unrefreshable heartbeat
	leaseMu.Lock()
	lost := leaseLostErr
	leaseMu.Unlock()
	if lost != nil {
		if errors.Is(lost, domain.ErrIdempotencyLeaseLost) {
			return nil, domain.ErrIdempotencyLeaseLost
		}
		return nil, lost
	}

	// If transaction completed all operations but commit returned an error (commit-ambiguous failure),
	// resolve and reconcile its outcome before allowing any new owner.
	if txCompleted {
		reconcileCtx, rCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer rCancel()
		rec, rErr := s.repos.Idempotency.GetIdempotency(reconcileCtx, req.IdempotencyKey)
		if rErr == nil && rec != nil {
			if rec.Status == domain.IdempotencyStatusCompleted || rec.Status == domain.IdempotencyStatusFailed {
				var cachedResp TransferResponse
				if jsonErr := json.Unmarshal([]byte(rec.ResponseBody), &cachedResp); jsonErr == nil {
					cachedResp.IsReplay = false
					cachedResp.ResponseCode = rec.ResponseCode
					success = true
					if rec.Status == domain.IdempotencyStatusFailed {
						return &cachedResp, domain.ErrInsufficientFunds
					}
					return &cachedResp, nil
				}
			} else if rec.Status == domain.IdempotencyStatusInProgress {
				transfer, tErr := s.repos.Transfers.GetTransferByID(reconcileCtx, transferID)
				if tErr == nil && transfer != nil {
					if transfer.Status == domain.TransferStatusProcessed {
						rec.Status = domain.IdempotencyStatusCompleted
						rec.TransferID = &transfer.ID
						rec.ResponseCode = http.StatusCreated
						rec.ResponseBody = string(respBytes)
						rec.OwnerToken = ownerToken
						if updateErr := s.repos.Idempotency.UpdateIdempotency(reconcileCtx, rec); updateErr != nil {
							if errors.Is(updateErr, domain.ErrIdempotencyLeaseLost) {
								return nil, domain.ErrIdempotencyLeaseLost
							}
							return nil, fmt.Errorf("failed to finalize idempotency record: %w", updateErr)
						}
						success = true
						return finalResponse, nil
					} else if transfer.Status == domain.TransferStatusFailed {
						rec.Status = domain.IdempotencyStatusFailed
						rec.TransferID = &transfer.ID
						rec.ResponseCode = http.StatusUnprocessableEntity
						rec.ResponseBody = string(respBytes)
						rec.OwnerToken = ownerToken
						if updateErr := s.repos.Idempotency.UpdateIdempotency(reconcileCtx, rec); updateErr != nil {
							if errors.Is(updateErr, domain.ErrIdempotencyLeaseLost) {
								return nil, domain.ErrIdempotencyLeaseLost
							}
							return nil, fmt.Errorf("failed to finalize idempotency record: %w", updateErr)
						}
						success = true
						return finalResponse, domain.ErrInsufficientFunds
					}
				} else if errors.Is(tErr, domain.ErrTransferNotFound) || errors.Is(tErr, pgx.ErrNoRows) {
					// Confirmed rolled back: safe to delete in-progress reservation
					_ = s.repos.Idempotency.DeleteInProgress(reconcileCtx, req.IdempotencyKey, ownerToken)
				}
				// If status is still ambiguous (e.g. operational DB error or timeout), keep the reservation intact!
			}
		}
	}

	return nil, txErr
}

func (s *TransferService) mapFailureReasonToError(reason *string, code int) error {
	if reason != nil {
		switch *reason {
		case "wallet not found":
			return domain.ErrWalletNotFound
		case domain.ErrCurrencyMismatch.Error():
			return domain.ErrCurrencyMismatch
		case domain.ErrBalanceOverflow.Error():
			return domain.ErrBalanceOverflow
		case "insufficient funds in source wallet":
			return domain.ErrInsufficientFunds
		}
	}
	switch code {
	case http.StatusNotFound:
		return domain.ErrWalletNotFound
	case http.StatusBadRequest:
		return errors.New("bad request")
	case http.StatusUnprocessableEntity:
		return domain.ErrInsufficientFunds
	default:
		return errors.New("transfer failed")
	}
}

// GetTransfer retrieves transfer details by ID.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (*domain.Transfer, error) {
	return s.repos.Transfers.GetTransferByID(ctx, id)
}
