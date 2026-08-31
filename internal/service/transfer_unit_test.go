package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/domain"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/service"
)

type stubStore struct {
	transfer    domain.Transfer
	transferErr error
	balance     int64
	balanceErr  error
	history     []domain.Transfer
	historyErr  error
}

func (s stubStore) CreateTransfer(context.Context, domain.CreateTransferRequest) (domain.Transfer, error) {
	return s.transfer, s.transferErr
}
func (s stubStore) Balance(context.Context, string) (int64, error) { return s.balance, s.balanceErr }
func (s stubStore) TransferHistory(context.Context, string, int) ([]domain.Transfer, error) {
	return s.history, s.historyErr
}

// TestServiceRejectsInvalidRequestsBeforeCallingTheStore verifies application validation.
func TestServiceRejectsInvalidRequestsBeforeCallingTheStore(t *testing.T) {
	svc := service.NewTransferService(stubStore{})
	if _, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{}); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.Balance(context.Background(), "invalid"); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.TransferHistory(context.Background(), "invalid", 1); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.TransferHistory(context.Background(), uuid.NewString(), 101); !errors.Is(err, domain.ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

// TestServiceDelegatesValidQueriesAndTransfers verifies service pass-through behavior.
func TestServiceDelegatesValidQueriesAndTransfers(t *testing.T) {
	from, to := uuid.NewString(), uuid.NewString()
	expected := domain.Transfer{ID: uuid.NewString(), Status: domain.Processed}
	svc := service.NewTransferService(stubStore{transfer: expected, balance: 42, history: []domain.Transfer{expected}})
	if transfer, err := svc.CreateTransfer(context.Background(), domain.CreateTransferRequest{IdempotencyKey: "key", FromWalletID: from, ToWalletID: to, Amount: 1}); err != nil || transfer.ID != expected.ID {
		t.Fatalf("transfer=%#v err=%v", transfer, err)
	}
	if balance, err := svc.Balance(context.Background(), from); err != nil || balance != 42 {
		t.Fatalf("balance=%d err=%v", balance, err)
	}
	if history, err := svc.TransferHistory(context.Background(), from, 1); err != nil || len(history) != 1 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}
