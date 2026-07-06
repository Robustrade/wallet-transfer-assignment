package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

type TransferService struct {
	repo *repository.TransferRepository
}

func NewTransferService(repo *repository.TransferRepository) *TransferService {
	return &TransferService{repo: repo}
}

// ExecuteTransfer processes the transfer request and handles idempotency recovery.
func (s *TransferService) ExecuteTransfer(ctx context.Context, req domain.TransferRequest) (*domain.TransferResponse, error) {
	// 1. Generate payload hash for tampering detection
	hashStr := fmt.Sprintf("%s:%s:%f", req.FromWalletId, req.ToWalletId, req.Amount)
	hash := sha256.Sum256([]byte(hashStr))
	reqHash := hex.EncodeToString(hash[:])

	// 2. Execute atomic transfer transaction
	resp, err := s.repo.ExecuteTransferTx(ctx, req, reqHash)

	// 3. Handle Idempotency Collision
	if err == domain.ErrDuplicateRequest {
		// A concurrent or previous request used the same idempotency key.
		// Fetch the cached response.
		log.Printf("[Service] ExecuteTransfer - Idempotency collision for key: %s", req.IdempotencyKey)
		cachedHash, cachedResp, fetchErr := s.repo.GetIdempotencyRecord(ctx, req.IdempotencyKey)
		if fetchErr != nil {
			log.Printf("[Service] ExecuteTransfer - Failed to fetch idempotency record: %v", fetchErr)
			return nil, fetchErr
		}

		// Ensure payload wasn't tampered with
		if cachedHash != reqHash {
			log.Printf("[Service] ExecuteTransfer - Idempotency hash mismatch! cached: %s, req: %s", cachedHash, reqHash)
			return nil, domain.ErrInvalidIdempotency
		}

		// Re-evaluate if the cached transaction actually failed business validation
		if cachedResp.Status == domain.TransferStatusFailed {
			return cachedResp, domain.ErrInsufficientFunds
		}

		return cachedResp, nil
	}

	if err != nil {
		log.Printf("[Service] ExecuteTransfer - Repository returned error: %v", err)
	}

	return resp, err
}
