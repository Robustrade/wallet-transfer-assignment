package test

import (
	"context"
	"testing"

	"github.com/gerry1818/wallet-transfer-assignment/internal/logger"
	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"github.com/gerry1818/wallet-transfer-assignment/internal/repository"
	"github.com/gerry1818/wallet-transfer-assignment/internal/service"
)

// =======================
// MOCK TX
// =======================
type MockTx struct {
	fromBalance int64
	toBalance   int64

	committed  bool
	rolledBack bool
}

func (m *MockTx) Commit(ctx context.Context) error {
	m.committed = true
	return nil
}

func (m *MockTx) Rollback(ctx context.Context) error {
	m.rolledBack = true
	return nil
}

func (m *MockTx) GetWalletForUpdate(ctx context.Context, id int64) (int64, error) {
	if id == 1 {
		return m.fromBalance, nil
	}
	return m.toBalance, nil
}

func (m *MockTx) UpdateWallet(ctx context.Context, id int64, balance int64) error {
	if id == 1 {
		m.fromBalance = balance
	} else {
		m.toBalance = balance
	}
	return nil
}

func (m *MockTx) GetBalance(ctx context.Context, walletID int64) (int64, error) {
	return m.GetWalletForUpdate(ctx, walletID)
}

func (m *MockTx) CreateTransfer(ctx context.Context, from, to, amount int64, key string) (int64, error) {
	return 1, nil
}

func (m *MockTx) UpdateTransferState(ctx context.Context, transferID int64, state string) error {
	return nil
}

func (m *MockTx) InsertLedgerEntry(ctx context.Context, walletID, transferID int64, typ string, amount int64) error {
	return nil
}

// =======================
// MOCK REPO
// =======================
type MockRepo struct {
	tx *MockTx

	// idempotency simulation
	claimOK bool
}

func (m *MockRepo) BeginTx(ctx context.Context) (repository.Tx, error) {
	if m.tx == nil {
		m.tx = &MockTx{
			fromBalance: 1000,
			toBalance:   500,
		}
	}
	return m.tx, nil
}

func (m *MockRepo) ClaimIdempotency(ctx context.Context, key, hash string) (bool, error) {
	return m.claimOK, nil
}

func (m *MockRepo) GetIdempotency(ctx context.Context, key string) (string, int, error) {
	return `{"transferId":1,"status":"PROCESSED"}`, 200, nil
}

func (m *MockRepo) UpdateIdempotency(ctx context.Context, key, status, resp string, code int) error {
	return nil
}

//
// =======================
// TESTS
// =======================
//

func TestIdempotencyHit(t *testing.T) {
	logger.Init()

	repo := &MockRepo{claimOK: false}
	svc := service.NewTransferService(repo)

	req := model.TransferRequest{
		IdempotencyKey: "abc",
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         100,
	}

	resp, code, err := svc.Transfer(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if code != 200 {
		t.Fatalf("expected 200 got %d", code)
	}

	if resp.TransferID != 1 {
		t.Errorf("expected transferID 1 got %d", resp.TransferID)
	}
}

func TestTransferSuccess(t *testing.T) {
	logger.Init()

	repo := &MockRepo{claimOK: true}
	svc := service.NewTransferService(repo)

	req := model.TransferRequest{
		IdempotencyKey: "txn-1",
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         100,
	}

	resp, code, err := svc.Transfer(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if code != 200 {
		t.Fatalf("expected 200 got %d", code)
	}

	if resp.TransferID != 1 {
		t.Errorf("expected transferID 1 got %d", resp.TransferID)
	}

	if repo.tx.fromBalance != 900 {
		t.Errorf("expected from balance 900 got %d", repo.tx.fromBalance)
	}

	if repo.tx.toBalance != 600 {
		t.Errorf("expected to balance 600 got %d", repo.tx.toBalance)
	}

	if !repo.tx.committed {
		t.Errorf("expected commit")
	}

	if repo.tx.rolledBack {
		t.Errorf("did not expect rollback")
	}
}

func TestTransferInsufficientBalance(t *testing.T) {
	logger.Init()

	repo := &MockRepo{
		claimOK: true,
		tx: &MockTx{
			fromBalance: 50,
			toBalance:   500,
		},
	}

	svc := service.NewTransferService(repo)

	req := model.TransferRequest{
		IdempotencyKey: "txn-2",
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         100,
	}

	_, code, err := svc.Transfer(context.Background(), req)

	if err == nil {
		t.Fatalf("expected error but got nil")
	}

	if code != 400 {
		t.Fatalf("expected 400 got %d", code)
	}

	if !repo.tx.rolledBack {
		t.Errorf("expected rollback")
	}

	if repo.tx.committed {
		t.Errorf("did not expect commit")
	}
}
