package service

import (
	"context"
	"errors"
	"testing"

	"wallet-transfer/internal/transfer/domain"
)

type fakeRepository struct {
	created domain.Transfer
	err     error
	calls   int
}

func (r *fakeRepository) Create(context.Context, domain.CreateRequest) (domain.Transfer, error) {
	r.calls++
	return r.created, r.err
}
func (r *fakeRepository) Get(context.Context, string) (domain.Transfer, error) {
	return domain.Transfer{}, nil
}
func (r *fakeRepository) ListAll(context.Context) ([]domain.Transfer, error) { return nil, nil }

func TestCreateValidatesBeforeRepository(t *testing.T) {
	repository := &fakeRepository{}
	service := New(repository)
	_, err := service.Create(context.Background(), domain.CreateRequest{Amount: 0})
	if !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if repository.calls != 0 {
		t.Fatal("repository was called for invalid input")
	}
}

func TestCreateDelegatesValidRequest(t *testing.T) {
	repository := &fakeRepository{created: domain.Transfer{ID: "transfer-1", Status: domain.Processed}}
	service := New(repository)
	transfer, err := service.Create(context.Background(), domain.CreateRequest{IdempotencyKey: "key", FromWalletID: "a", ToWalletID: "b", Amount: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.ID != "transfer-1" || repository.calls != 1 {
		t.Fatalf("unexpected result: %+v calls=%d", transfer, repository.calls)
	}
}
