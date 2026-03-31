package service

import (
	"context"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// BenchmarkTransferService scales memory and CPU to prove the orchestration layer is highly performant.
// We mock the repository to specifically benchmark the Application Layer execution time.
func BenchmarkTransferService(b *testing.B) {
	mockRepo := &mockRepository{
		getIdempotentFunc: func(ctx context.Context, key string) (*model.Transfer, error) {
			return nil, repository.ErrNotFound // Assume cache miss for new transfers
		},
		executeTxFunc: func(ctx context.Context, transfer *model.Transfer) error {
			return nil // Assume fast DB execution
		},
	}

	svc := NewTransferService(mockRepo)
	ctx := context.Background()
	req := TransferRequest{
		IdempotencyKey: "bench_key",
		FromWalletID:   "w1",
		ToWalletID:     "w2",
		Amount:         100,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := svc.Transfer(ctx, req)
		if err != nil && err.Error() != "record not found" {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
