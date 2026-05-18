package service

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/wallet-transfer-assignment/internal/model"
	"github.com/wallet-transfer-assignment/internal/repository"
)

type Service struct {
	repo *repository.Repository
}

func NewService(repo *repository.Repository) *Service {
	return &Service{repo: repo}
}

type TransferResponse struct {
	TransferID string `json:"transferId"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

type ServiceResult struct {
	HTTPStatus int
	Response   json.RawMessage
	Error      error
}

func (s *Service) ProcessTransfer(ctx context.Context, req *model.CreateTransferRequest) ServiceResult {
	// 1. Lock idempotency key
	idk, err := s.repo.LockIdempotencyKey(ctx, req.IdempotencyKey)
	if err == repository.ErrIdempotencyKeyLocked {
		return ServiceResult{HTTPStatus: http.StatusConflict, Error: err}
	} else if err != nil {
		return ServiceResult{HTTPStatus: http.StatusInternalServerError, Error: err}
	}

	// 2. Check if it's already processed
	if idk != nil && idk.ResponseCode != nil {
		return ServiceResult{HTTPStatus: *idk.ResponseCode, Response: idk.ResponseBody}
	}

	// 3. Execute transfer
	transferID := uuid.New().String()
	transfer := &model.Transfer{
		ID:             transferID,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	}

	err = s.repo.ExecuteTransfer(ctx, transfer)

	var status int
	var resp TransferResponse

	if err == repository.ErrInsufficientFunds {
		status = http.StatusBadRequest
		resp = TransferResponse{TransferID: transferID, Status: "FAILED", Message: "Insufficient funds"}
	} else if err == repository.ErrWalletNotFound {
		status = http.StatusBadRequest
		resp = TransferResponse{TransferID: transferID, Status: "FAILED", Message: "Wallet not found"}
	} else if err != nil {
		_ = s.repo.UpdateIdempotencyKey(ctx, req.IdempotencyKey, http.StatusInternalServerError, []byte(`{"error":"internal server error"}`))
		return ServiceResult{HTTPStatus: http.StatusInternalServerError, Error: err}
	} else {
		status = http.StatusOK
		resp = TransferResponse{TransferID: transferID, Status: "PROCESSED"}
	}

	respBody, _ := json.Marshal(resp)
	_ = s.repo.UpdateIdempotencyKey(ctx, req.IdempotencyKey, status, respBody)

	return ServiceResult{HTTPStatus: status, Response: respBody}
}
