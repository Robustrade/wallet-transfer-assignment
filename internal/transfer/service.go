package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (Outcome, error) {
	if strings.TrimSpace(request.IdempotencyKey) == "" {
		return Outcome{}, invalid("idempotencyKey is required")
	}
	if strings.TrimSpace(request.FromWalletID) == "" {
		return Outcome{}, invalid("fromWalletId is required")
	}
	if strings.TrimSpace(request.ToWalletID) == "" {
		return Outcome{}, invalid("toWalletId is required")
	}
	if request.FromWalletID == request.ToWalletID {
		return Outcome{}, invalid("fromWalletId and toWalletId must be different")
	}
	if request.Amount <= 0 {
		return Outcome{}, invalid("amount must be a positive integer in minor currency units")
	}

	canonicalRequest, err := json.Marshal(struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{
		FromWalletID: request.FromWalletID,
		ToWalletID:   request.ToWalletID,
		Amount:       request.Amount,
	})
	if err != nil {
		return Outcome{}, err
	}
	requestHash := sha256.Sum256(canonicalRequest)

	return s.repository.Create(ctx, request, requestHash)
}

func invalid(message string) error {
	return &Error{
		StatusCode: 400,
		Code:       "invalid_request",
		Message:    message,
	}
}
