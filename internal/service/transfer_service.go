package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/sonalijais/wallet-transfer-assignment/internal/domain"
	"github.com/sonalijais/wallet-transfer-assignment/internal/repository"
	"github.com/sonalijais/wallet-transfer-assignment/internal/utility"
)

type Service struct {
	Repo *repository.Repository
	DB   *sql.DB
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         float64
}

type Response struct {
	TransferID string `json:"transfer_id"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
}

func (s *Service) CreateTransfer(ctx context.Context, req CreateTransferRequest) (*Response, error) {
	rId := utility.GetRequestID(ctx)
	log.Printf("[SERVICE_START] request_id=%s idempotency_key=%s", rId, req.IdempotencyKey)

	log.Printf("[TX_BEGIN] request_id=%s starting DB transaction", rId)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[TX_BEGIN_FAILED] request_id=%s error=%v", rId, err)
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Printf("[TX_ROLLBACK] request_id=%s rolling back transaction", rId)

			if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
				log.Printf("[TX_ROLLBACK_ERROR] request_id=%s err=%v", rId, rbErr)
			}
		}
	}()

	transfer := &domain.Transfer{
		ID:             uuid.New().String(),
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         domain.Pending,
		IdempotencyKey: req.IdempotencyKey,
	}

	if err := transfer.Validate(); err != nil {
		log.Printf("[VALIDATION_FAILED] request_id=%s reason=%v", rId, err)
		return nil, err
	}

	reqBytes, _ := json.Marshal(req)
	hash := s.hashRequest(req)

	log.Printf("[IDEMPOTENCY_CHECK] request_id=%s inserting key=%s", rId, req.IdempotencyKey)

	// 1. insert idempotency row
	inserted, err := s.Repo.InsertIdempotency(ctx, tx, req.IdempotencyKey, hash, reqBytes)
	if err != nil {
		log.Printf("[IDEMPOTENCY_ERROR] request_id=%s error=%v", rId, err)
		return nil, fmt.Errorf("failed to insert idempotency: %w", err)
	}

	if !inserted {
		log.Printf("[IDEMPOTENCY_DUPLICATE] request_id=%s key=%s", rId, req.IdempotencyKey)

		existingHash, err := s.Repo.GetRequestHash(ctx, tx, req.IdempotencyKey)
		if err != nil {
			log.Printf("[DB_ERROR] request_id=%s operation=GetRequestHash error=%v", rId, err)
			return nil, err
		}

		if existingHash != hash {
			log.Printf(
				"[IDEMPOTENCY_CONFLICT] request_id=%s key=%s reason=HASH_MISMATCH",
				rId,
				req.IdempotencyKey,
			)
			return nil, utility.ErrIdempotencyConflict
		}

		// duplicate request → return stored response
		existingResp, err := s.Repo.GetIdempotency(ctx, tx, req.IdempotencyKey)
		if err != nil {
			log.Printf("[DB_ERROR] request_id=%s operation=GetIdempotency error=%v", rId, err)
			return nil, fmt.Errorf("failed to fetch idempotency response: %w", err)
		}

		if len(existingResp) == 0 {
			log.Printf(
				"[IDEMPOTENCY_RETRY] request_id=%s key=%s reason=RESPONSE_NOT_READY",
				rId,
				req.IdempotencyKey,
			)
			return nil, utility.ErrRetryLater
		}

		var resp Response
		if err := json.Unmarshal(existingResp, &resp); err != nil {
			log.Printf(
				"[IDEMPOTENCY_UNMARSHAL_FAILED] request_id=%s key=%s error=%v",
				rId,
				req.IdempotencyKey,
				err,
			)
			return nil, fmt.Errorf("failed to unmarshal idempotency response: %w", err)
		}

		return &resp, nil
	}

	// 2. Lock wallets
	balances := map[string]float64{}

	ids := []string{req.FromWalletID, req.ToWalletID}
	sort.Strings(ids)

	for _, id := range ids {
		log.Printf("[LOCK_WALLETS] request_id=%s wallet id=%s ", rId, id)
		bal, err := s.Repo.GetWalletForUpdate(ctx, tx, id)
		if err != nil {
			log.Printf("[DB_ERROR] request_id=%s operation=GetWalletForUpdate error=%v", rId, err)
			return nil, fmt.Errorf("failed to lock wallet %s: %w", id, err)
		}
		balances[id] = bal
	}

	fromWallet := &domain.Wallet{
		ID:      req.FromWalletID,
		Balance: balances[req.FromWalletID],
	}

	log.Printf("[BALANCE_CHECK] request_id=%s from_balance=%f amount=%f", rId, fromWallet.Balance, req.Amount)

	if !fromWallet.CanDebit(transfer.Amount) {
		log.Printf("[BUSINESS_FAIL] request_id=%s reason=INSUFFICIENT_FUNDS", rId)

		transfer.Status = domain.Failed

		// 1. Persist FAILED transfer
		if err := s.Repo.CreateTransfer(ctx, tx,
			transfer.ID,
			transfer.FromWalletID,
			transfer.ToWalletID,
			transfer.Amount,
			string(transfer.Status),
			transfer.IdempotencyKey,
		); err != nil {
			log.Printf("[DB_ERROR] request_id=%s operation=CreateTransfer error=%v", rId, err)
			return nil, fmt.Errorf("failed to create failed transfer: %w", err)
		}

		// 2. Prepare response
		resp := &Response{
			TransferID: transfer.ID,
			Status:     string(transfer.Status),
			Reason:     utility.ErrInsufficientFunds.Error(),
			Message:    "wallet balance is too low",
		}

		// 3. Store idempotency response
		bytes, err := json.Marshal(resp)
		if err != nil {
			log.Printf(
				"[RESPONSE_MARSHAL_FAILED] request_id=%s transfer_id=%s error=%v",
				rId,
				transfer.ID,
				err,
			)
			return nil, fmt.Errorf("failed to marshal failure response: %w", err)
		}

		if err := s.Repo.UpdateIdempotency(ctx, tx, req.IdempotencyKey, bytes); err != nil {
			log.Printf("[DB_ERROR] request_id=%s operation=UpdateIdempotency error=%v", rId, err)
			return nil, fmt.Errorf("failed to update idempotency: %w", err)
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[TX_COMMIT_FAILED] request_id=%s error=%v", rId, err)
			return nil, err
		}
		return resp, nil
	}

	// 3. Create transfer
	log.Printf("[TRANSFER_CREATE] request_id=%s transfer_id=%s status=PENDING", rId, transfer.ID)

	if err := s.Repo.CreateTransfer(ctx, tx,
		transfer.ID,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		string(transfer.Status),
		transfer.IdempotencyKey,
	); err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=CreateTransfer error=%v", rId, err)
		return nil, fmt.Errorf("failed to create transfer: %w", err)
	}

	// 4. Ledger
	log.Printf("[LEDGER_WRITE] request_id=%s transfer_id=%s debit=%f credit=%f",
		rId, transfer.ID, req.Amount, req.Amount)

	if err := s.Repo.CreateLedgerEntry(ctx, tx, transfer.FromWalletID, transfer.ID, string(domain.Debit), transfer.Amount); err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=CreateLedgerEntry error=%v", rId, err)
		return nil, fmt.Errorf("failed to create debit entry: %w", err)
	}

	if err := s.Repo.CreateLedgerEntry(ctx, tx, transfer.ToWalletID, transfer.ID, string(domain.Credit), transfer.Amount); err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=CreateLedgerEntry error=%v", rId, err)
		return nil, fmt.Errorf("failed to create credit entry: %w", err)
	}

	// 5. Update balances
	log.Printf("[BALANCE_UPDATE] request_id=%s from_wallet=%s -%f to_wallet=%s +%f",
		rId, req.FromWalletID, req.Amount, req.ToWalletID, req.Amount)

	err = s.Repo.UpdateBalance(ctx, tx, transfer.FromWalletID, -transfer.Amount)
	if err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=UpdateBalance error=%v", rId, err)
		return nil, fmt.Errorf("failed to update balance : %w", err)
	}

	err = s.Repo.UpdateBalance(ctx, tx, transfer.ToWalletID, transfer.Amount)
	if err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=UpdateBalance error=%v", rId, err)
		return nil, fmt.Errorf("failed to update balance : %w", err)
	}

	// state transition
	if !transfer.Status.CanTransitionTo(domain.Processed) {
		log.Printf(
			"[INVALID_STATE_TRANSITION] request_id=%s transfer_id=%s from=%s to=%s",
			rId,
			transfer.ID,
			transfer.Status,
			domain.Processed,
		)
		return nil, fmt.Errorf("invalid state transition")
	}

	transfer.Status = domain.Processed

	// 6. Mark processed
	err = s.Repo.UpdateTransferStatus(ctx, tx, transfer.ID, string(transfer.Status))
	if err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=UpdateTransferStatus error=%v", rId, err)
		return nil, fmt.Errorf("failed to update transfer status : %w", err)
	}

	resp := &Response{
		TransferID: transfer.ID,
		Status:     string(domain.Processed),
		Message:    "transfer completed successfully",
	}

	bytes, err := json.Marshal(resp)
	if err != nil {
		log.Printf(
			"[RESPONSE_MARSHAL_FAILED] request_id=%s transfer_id=%s error=%v",
			rId,
			transfer.ID,
			err,
		)
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	log.Printf("[IDEMPOTENCY_STORE] request_id=%s storing response", rId)

	err = s.Repo.UpdateIdempotency(ctx, tx, req.IdempotencyKey, bytes)
	if err != nil {
		log.Printf("[DB_ERROR] request_id=%s operation=UpdateIdempotency error=%v", rId, err)
		return nil, fmt.Errorf("failed to update idempotency : %w", err)
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[TX_COMMIT_FAILED] request_id=%s error=%v", rId, err)
		return nil, err
	}

	log.Printf("[TX_COMMIT] request_id=%s success", rId)
	return resp, nil
}

func (s *Service) hashRequest(req CreateTransferRequest) string {
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (r *CreateTransferRequest) Validate() error {
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return fmt.Errorf("idempotencyKey is required")
	}
	if strings.TrimSpace(r.FromWalletID) == "" || strings.TrimSpace(r.ToWalletID) == "" {
		return fmt.Errorf("wallet ids cannot be empty")
	}
	if r.Amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	if r.FromWalletID == r.ToWalletID {
		return fmt.Errorf("cannot transfer to same wallet")
	}
	return nil
}
