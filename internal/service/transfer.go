package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"wallet/internal/constants"
	"wallet/internal/domain"
	"wallet/internal/dto"
)

// outcome is one completed attempt: the client-facing result, plus the log
// line the attempt has earned once - and only once - its transaction commits.
// Logging before the commit would let a transaction that never lands claim an
// outcome the database does not have.
type outcome struct {
	result  Result
	logLine func(ctx context.Context)
}

// Execute attempts one transfer and returns the outcome to send to the client.
//
// Everything - claiming the idempotency key, both balance updates, the ledger
// pair, the transfer's state, and the recorded response - happens in ONE
// database transaction. The response is only returned after that transaction
// commits, so an outcome can never be observed without its effects.
//
// The transaction's statement order is the concurrency design:
//
//  1. claim the idempotency key (the unique index decides the winner)
//  2. lock both wallets in ascending id order (deadlock-free ordering)
//  3. insert the transfer as PENDING
//  4. debit, credit, and write the ledger pair (PROCESSED branch), or mark the
//     transfer FAILED and record why
//  5. complete the idempotency record with the outcome, then COMMIT
//
// Every branch either commits a complete outcome or leaves no trace at all.
func (s *Service) Execute(ctx context.Context, req dto.TransferRequest) (Result, error) {
	if err := validate(req); err != nil {
		return Result{}, err
	}

	canonical, err := canonicalRequest(req)
	if err != nil {
		return Result{}, err
	}

	var out outcome
	err = s.store.InTx(ctx, func(ctx context.Context, tx Tx) error {
		o, err := s.attempt(ctx, tx, req, canonical)
		if err != nil {
			return err
		}
		out = o
		return nil
	})
	if err != nil {
		// A failed COMMIT is the one case where the outcome is unknown: the
		// server may or may not have applied it. Resolve by reading the
		// outcome back before telling the client anything.
		if errors.Is(err, domain.ErrCommitOutcomeUnknown) {
			result, resolveErr := s.resolveUnknownOutcome(ctx, req.IdempotencyKey, canonical, err)
			if resolveErr == nil && out.logLine != nil {
				// The commit did land after all: the outcome log is owed.
				out.logLine(ctx)
			}
			return result, resolveErr
		}
		return Result{}, err
	}

	// The transaction committed: only now does the outcome log describe
	// reality.
	if out.logLine != nil {
		out.logLine(ctx)
	}
	return out.result, nil
}

// attempt runs the transfer's transaction body; it is called exactly once per
// Execute and must either return an error (rolling everything back) or a
// complete outcome that the surrounding transaction will commit.
func (s *Service) attempt(ctx context.Context, tx Tx, req dto.TransferRequest, canonical []byte) (outcome, error) {
	// 1. One request wins the key; every duplicate loses here.
	claimed, err := tx.ClaimIdempotency(ctx, req.IdempotencyKey, canonical)
	if err != nil {
		return outcome{}, err
	}
	if !claimed {
		return s.replay(ctx, tx, req, canonical)
	}

	// 2. Wallet locks, taken in a single ordered statement.
	locked, err := tx.LockWallets(ctx, []string{req.FromWalletID, req.ToWalletID})
	if err != nil {
		return outcome{}, err
	}
	if len(locked) != 2 {
		return s.recordNotFound(ctx, tx, req, locked)
	}

	// 3. The transfer exists before any money moves, so every balance change
	// is attributable to a transfer row.
	transfer, err := tx.CreateTransfer(ctx, req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		return outcome{}, err
	}
	// The destination row is locked, so this preflight remains valid through
	// the subsequent credit. Rejecting before the debit keeps the failure a
	// normal recorded outcome rather than relying on PostgreSQL's bigint
	// overflow error to abort the transaction.
	if balanceOf(locked, req.ToWalletID) > int64(^uint64(0)>>1)-req.Amount {
		return s.recordBalanceOverflow(ctx, tx, req, transfer)
	}

	// 4a. The debit's guard clause decides sufficiency.
	debited, err := tx.DebitWallet(ctx, req.FromWalletID, req.Amount)
	if err != nil {
		return outcome{}, err
	}
	if !debited {
		return s.recordInsufficientFunds(ctx, tx, req, transfer, locked)
	}

	// 4b. Credit and ledger pair. Both sides of the ledger agree by
	// construction: they are written from the same amount in one statement.
	if err := tx.CreditWallet(ctx, req.ToWalletID, req.Amount); err != nil {
		return outcome{}, err
	}
	if err := tx.InsertLedgerEntries(ctx, transfer.ID, req.FromWalletID, req.ToWalletID, req.Amount); err != nil {
		return outcome{}, err
	}

	// 5. The transfer becomes visible as PROCESSED only now, in the same
	// transaction that moved the money.
	if err := tx.MarkProcessed(ctx, transfer.ID); err != nil {
		return outcome{}, err
	}
	transfer.Status = constants.TransferProcessed

	body, err := marshal(dto.NewTransferResponse(transfer))
	if err != nil {
		return outcome{}, err
	}
	if err := tx.CompleteIdempotency(ctx, req.IdempotencyKey, &transfer.ID, http.StatusCreated, body); err != nil {
		return outcome{}, err
	}

	return outcome{
		result: Result{Status: http.StatusCreated, Body: body},
		logLine: func(ctx context.Context) {
			s.logger.InfoContext(ctx, "transfer processed",
				"transfer_id", transfer.ID,
				"from_wallet_id", req.FromWalletID,
				"to_wallet_id", req.ToWalletID,
				"amount", req.Amount)
		},
	}, nil
}

// replay answers a duplicate request from the record the first request left
// behind. The record is complete by construction: the only transaction that
// can conflict on a key is one that has already committed its outcome.
func (s *Service) replay(ctx context.Context, tx Tx, req dto.TransferRequest, canonical []byte) (outcome, error) {
	record, found, err := tx.GetIdempotencyRecord(ctx, req.IdempotencyKey, canonical)
	if err != nil {
		return outcome{}, err
	}
	if !found || !record.Complete() {
		// Unreachable: a conflict implies a committed, completed record.
		s.logger.ErrorContext(ctx, "idempotency conflict without a complete record",
			"idempotency_key", req.IdempotencyKey)
		return outcome{}, fmt.Errorf("%w: key %s", domain.ErrIncompleteIdempotency, req.IdempotencyKey)
	}

	// Same key, different payload: never silently return the first result.
	if !record.RequestMatches {
		s.logger.WarnContext(ctx, "idempotency key reused with a different request",
			"idempotency_key", req.IdempotencyKey,
			"transfer_id", deref(record.TransferID))
		return outcome{}, fmt.Errorf("%w: key %s", domain.ErrKeyReused, req.IdempotencyKey)
	}

	// This log describes a record that was already committed before this
	// transaction began, so it is safe to emit here rather than post-commit.
	s.logger.InfoContext(ctx, "replayed recorded outcome",
		"idempotency_key", req.IdempotencyKey,
		"status", *record.ResponseStatus)

	// Replays return the stored status and body byte-for-byte.
	return outcome{result: Result{Status: *record.ResponseStatus, Body: record.ResponseBody}}, nil
}

// recordNotFound completes the request with a recorded 404. No transfer row
// is created: there is no wallet to move money between.
func (s *Service) recordNotFound(ctx context.Context, tx Tx, req dto.TransferRequest, locked []domain.Wallet) (outcome, error) {
	body, err := marshal(dto.NewErrorResponse(constants.CodeWalletNotFound,
		fmt.Sprintf("wallet not found: %s", strings.Join(missingWallets(req, locked), ", "))))
	if err != nil {
		return outcome{}, err
	}
	if err := tx.CompleteIdempotency(ctx, req.IdempotencyKey, nil, http.StatusNotFound, body); err != nil {
		return outcome{}, err
	}

	return outcome{
		result: Result{Status: http.StatusNotFound, Body: body},
		logLine: func(ctx context.Context) {
			s.logger.InfoContext(ctx, "transfer rejected: wallet not found",
				"idempotency_key", req.IdempotencyKey,
				"from_wallet_id", req.FromWalletID,
				"to_wallet_id", req.ToWalletID)
		},
	}, nil
}

// recordInsufficientFunds completes the request with a recorded 422 and a
// FAILED transfer. No money moves and no ledger entry is written.
func (s *Service) recordInsufficientFunds(ctx context.Context, tx Tx, req dto.TransferRequest, transfer domain.Transfer, locked []domain.Wallet) (outcome, error) {
	if err := tx.MarkFailed(ctx, transfer.ID, constants.FailureInsufficientFunds); err != nil {
		return outcome{}, err
	}

	available := balanceOf(locked, req.FromWalletID)
	body, err := marshal(dto.ErrorResponse{Error: dto.ErrorBody{
		Code: constants.CodeInsufficientFunds,
		Message: fmt.Sprintf("wallet %s has insufficient funds: balance %d, requested %d",
			req.FromWalletID, available, req.Amount),
		TransferID: transfer.ID,
	}})
	if err != nil {
		return outcome{}, err
	}
	if err := tx.CompleteIdempotency(ctx, req.IdempotencyKey, &transfer.ID, http.StatusUnprocessableEntity, body); err != nil {
		return outcome{}, err
	}

	return outcome{
		result: Result{Status: http.StatusUnprocessableEntity, Body: body},
		logLine: func(ctx context.Context) {
			s.logger.InfoContext(ctx, "transfer failed: insufficient funds",
				"transfer_id", transfer.ID,
				"idempotency_key", req.IdempotencyKey,
				"from_wallet_id", req.FromWalletID,
				"balance", available,
				"amount", req.Amount)
		},
	}, nil
}

// recordBalanceOverflow rejects a transfer whose destination cannot represent
// the credited balance in PostgreSQL's bigint range. It runs before the debit,
// so no compensating balance update is needed.
func (s *Service) recordBalanceOverflow(ctx context.Context, tx Tx, req dto.TransferRequest, transfer domain.Transfer) (outcome, error) {
	if err := tx.MarkFailed(ctx, transfer.ID, constants.FailureBalanceOverflow); err != nil {
		return outcome{}, err
	}

	body, err := marshal(dto.ErrorResponse{Error: dto.ErrorBody{
		Code:       constants.CodeBalanceOverflow,
		Message:    fmt.Sprintf("wallet %s cannot receive amount %d without exceeding the maximum balance", req.ToWalletID, req.Amount),
		TransferID: transfer.ID,
	}})
	if err != nil {
		return outcome{}, err
	}
	if err := tx.CompleteIdempotency(ctx, req.IdempotencyKey, &transfer.ID, http.StatusUnprocessableEntity, body); err != nil {
		return outcome{}, err
	}

	return outcome{
		result: Result{Status: http.StatusUnprocessableEntity, Body: body},
		logLine: func(ctx context.Context) {
			s.logger.InfoContext(ctx, "transfer failed: destination balance overflow",
				"transfer_id", transfer.ID,
				"idempotency_key", req.IdempotencyKey,
				"to_wallet_id", req.ToWalletID,
				"amount", req.Amount)
		},
	}, nil
}

// resolveUnknownOutcome handles a COMMIT whose result we could not observe.
// It reads the record back on a fresh connection: if the outcome was in fact
// committed, the client gets the recorded response; otherwise the outcome is
// reported as unknown (503) and the client may safely retry with the same key.
func (s *Service) resolveUnknownOutcome(ctx context.Context, key string, canonical []byte, cause error) (Result, error) {
	// The commit's fate is exactly what this read must discover, so it runs
	// detached from the request context: a client that has hung up or timed
	// out must not be able to turn a resolvable outcome into an unknown one.
	readCtx := context.WithoutCancel(ctx)
	record, found, err := s.store.GetIdempotencyRecord(readCtx, key, canonical)
	if err == nil && found && record.Complete() && record.RequestMatches {
		s.logger.WarnContext(readCtx, "commit outcome resolved by reading the record back",
			"idempotency_key", key, "status", *record.ResponseStatus)
		return Result{Status: *record.ResponseStatus, Body: record.ResponseBody}, nil
	}

	s.logger.ErrorContext(readCtx, "commit outcome unknown",
		"idempotency_key", key, "error", cause, "resolve_error", err)
	return Result{}, fmt.Errorf("%w: %w", domain.ErrCommitOutcomeUnknown, cause)
}

func missingWallets(req dto.TransferRequest, locked []domain.Wallet) []string {
	present := make(map[string]bool, len(locked))
	for _, w := range locked {
		present[w.ID] = true
	}
	var missing []string
	for _, id := range []string{req.FromWalletID, req.ToWalletID} {
		if !present[id] {
			missing = append(missing, id)
		}
	}
	return missing
}

func balanceOf(locked []domain.Wallet, id string) int64 {
	for _, w := range locked {
		if w.ID == id {
			return w.Balance
		}
	}
	return 0
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func marshal(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal response: %w", err)
	}
	return body, nil
}
