package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

func validateCreateTransferInput(input CreateTransferInput) error {
	return domain.ValidateTransferRequest(input.FromWalletID, input.ToWalletID, input.Amount)
}

func hashCreateTransferInput(input CreateTransferInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf(
		"%s|%s|%s|%d",
		input.IdempotencyKey,
		input.FromWalletID,
		input.ToWalletID,
		input.Amount,
	)))
	return hex.EncodeToString(sum[:])
}

func (s *TransferServiceImpl) lookupIdempotentResult(
	ctx context.Context,
	input CreateTransferInput,
) (*CreateTransferResult, bool, error) {
	record, err := s.store.Idempotency().GetByKey(ctx, input.IdempotencyKey)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	requestHash := hashCreateTransferInput(input)
	if record.RequestHash != requestHash {
		return nil, false, domain.ErrIdempotencyConflict
	}

	result, err := decodeTransferResult(record.ResponseBody)
	if err != nil {
		return nil, false, fmt.Errorf("decode idempotent response: %w", err)
	}
	return result, true, nil
}

func (s *TransferServiceImpl) saveIdempotencyRecord(
	ctx context.Context,
	input CreateTransferInput,
	result *CreateTransferResult,
) error {
	body, err := encodeTransferResult(result)
	if err != nil {
		return err
	}

	return s.store.Idempotency().Save(ctx, &domain.IdempotencyRecord{
		Key:            input.IdempotencyKey,
		TransferID:     result.TransferID,
		RequestHash:    hashCreateTransferInput(input),
		ResponseStatus: http.StatusCreated,
		ResponseBody:   body,
	})
}

func encodeTransferResult(result *CreateTransferResult) (string, error) {
	payload, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal transfer result: %w", err)
	}
	return string(payload), nil
}

func decodeTransferResult(body string) (*CreateTransferResult, error) {
	var result CreateTransferResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, err
	}
	return &result, nil
}
