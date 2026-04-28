package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"wallet-transfer/internal/transfer"
	transferService "wallet-transfer/internal/transfer/service"
	"wallet-transfer/internal/transfer/static"
	"wallet-transfer/internal/wallet"
	"wallet-transfer/pkg/apperror"
	"wallet-transfer/pkg/database"
)

// ─── Mocks ────────────────────────────────────────────────────────────────────

type MockTransferRepository struct{ mock.Mock }

func (m *MockTransferRepository) CreateTransfer(ctx context.Context, db database.DBTX, t *transfer.Transfer) error {
	args := m.Called(ctx, db, t)
	return args.Error(0)
}
func (m *MockTransferRepository) GetTransferByID(ctx context.Context, db database.DBTX, id string) (*transfer.Transfer, error) {
	args := m.Called(ctx, db, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*transfer.Transfer), args.Error(1)
}
func (m *MockTransferRepository) GetTransferByIdempotencyKey(ctx context.Context, db database.DBTX, key string) (*transfer.Transfer, error) {
	args := m.Called(ctx, db, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*transfer.Transfer), args.Error(1)
}
func (m *MockTransferRepository) UpdateTransferStatus(ctx context.Context, db database.DBTX, id, status, reason, updatedBy string) error {
	args := m.Called(ctx, db, id, status, reason, updatedBy)
	return args.Error(0)
}
func (m *MockTransferRepository) GetAllTransfers(ctx context.Context, db database.DBTX) ([]*transfer.Transfer, error) {
	args := m.Called(ctx, db)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*transfer.Transfer), args.Error(1)
}
func (m *MockTransferRepository) CreateLedgerEntry(ctx context.Context, db database.DBTX, e *transfer.LedgerEntry) error {
	args := m.Called(ctx, db, e)
	return args.Error(0)
}
func (m *MockTransferRepository) GetLedgerEntriesByTransferID(ctx context.Context, db database.DBTX, id string) ([]*transfer.LedgerEntry, error) {
	args := m.Called(ctx, db, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*transfer.LedgerEntry), args.Error(1)
}
func (m *MockTransferRepository) GetLedgerEntriesByWalletID(ctx context.Context, db database.DBTX, id string) ([]*transfer.LedgerEntry, error) {
	args := m.Called(ctx, db, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*transfer.LedgerEntry), args.Error(1)
}

type MockWalletRepository struct{ mock.Mock }

func (m *MockWalletRepository) Create(ctx context.Context, db database.DBTX, w *wallet.Wallet) error {
	return m.Called(ctx, db, w).Error(0)
}
func (m *MockWalletRepository) GetByID(ctx context.Context, db database.DBTX, id string) (*wallet.Wallet, error) {
	args := m.Called(ctx, db, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*wallet.Wallet), args.Error(1)
}
func (m *MockWalletRepository) GetByIDForUpdate(ctx context.Context, db database.DBTX, id string) (*wallet.Wallet, error) {
	args := m.Called(ctx, db, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*wallet.Wallet), args.Error(1)
}
func (m *MockWalletRepository) UpdateBalance(ctx context.Context, db database.DBTX, id string, balance float64, updatedBy string) error {
	return m.Called(ctx, db, id, balance, updatedBy).Error(0)
}
func (m *MockWalletRepository) GetAll(ctx context.Context, db database.DBTX) ([]*wallet.Wallet, error) {
	args := m.Called(ctx, db)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*wallet.Wallet), args.Error(1)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newProcessedTransfer(id, from, to string, amount float64) *transfer.Transfer {
	return &transfer.Transfer{
		ID:           id,
		FromWalletID: from,
		ToWalletID:   to,
		Amount:       amount,
		Status:       static.StatusProcessed,
	}
}

// ─── Validation tests (no DB needed) ──────────────────────────────────────────

func TestCreateTransfer_Validation_SameWallet(t *testing.T) {
	tr := new(MockTransferRepository)
	wr := new(MockWalletRepository)
	// No pool needed — validation fails before any DB call.
	svc := transferService.NewTransferService(nil, tr, wr)

	_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_1",
		Amount:       100,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
	assert.Equal(t, static.SameWalletError, appErr.Message)
}

func TestCreateTransfer_Validation_ZeroAmount(t *testing.T) {
	svc := transferService.NewTransferService(nil, new(MockTransferRepository), new(MockWalletRepository))

	_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       0,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestCreateTransfer_Validation_NegativeAmount(t *testing.T) {
	svc := transferService.NewTransferService(nil, new(MockTransferRepository), new(MockWalletRepository))

	_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       -50,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestCreateTransfer_Validation_MissingFromWallet(t *testing.T) {
	svc := transferService.NewTransferService(nil, new(MockTransferRepository), new(MockWalletRepository))

	_, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		ToWalletID: "wallet_2",
		Amount:     100,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

// ─── Idempotency tests ─────────────────────────────────────────────────────────

func TestCreateTransfer_Idempotency_DuplicateKeyReturnsOriginal(t *testing.T) {
	tr := new(MockTransferRepository)
	wr := new(MockWalletRepository)

	existingTransfer := newProcessedTransfer("txn-abc", "wallet_1", "wallet_2", 100)
	existingTransfer.IdempotencyKey = "key-123"

	existingLedger := []*transfer.LedgerEntry{
		{ID: "l1", WalletID: "wallet_1", TransferID: "txn-abc", Type: static.TypeDebit, Amount: 100},
		{ID: "l2", WalletID: "wallet_2", TransferID: "txn-abc", Type: static.TypeCredit, Amount: 100},
	}

	tr.On("GetTransferByIdempotencyKey", mock.Anything, mock.Anything, "key-123").
		Return(existingTransfer, nil)
	tr.On("GetLedgerEntriesByTransferID", mock.Anything, mock.Anything, "txn-abc").
		Return(existingLedger, nil)

	svc := transferService.NewTransferService(nil, tr, wr)

	resp, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		IdempotencyKey: "key-123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "txn-abc", resp.Transfer.ID)
	assert.Equal(t, static.StatusProcessed, resp.Transfer.Status)
	assert.Len(t, resp.Ledger, 2)

	// Ensure no new transfer was created (no CreateTransfer call).
	tr.AssertNotCalled(t, "CreateTransfer")
}

func TestCreateTransfer_Idempotency_FailedTransferReturnsSameFailure(t *testing.T) {
	tr := new(MockTransferRepository)
	wr := new(MockWalletRepository)

	failedTransfer := &transfer.Transfer{
		ID:             "txn-failed",
		IdempotencyKey: "key-fail",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         9999,
		Status:         static.StatusFailed,
		FailureReason:  static.InsufficientBalance,
	}

	tr.On("GetTransferByIdempotencyKey", mock.Anything, mock.Anything, "key-fail").
		Return(failedTransfer, nil)
	tr.On("GetLedgerEntriesByTransferID", mock.Anything, mock.Anything, "txn-failed").
		Return([]*transfer.LedgerEntry{}, nil)

	svc := transferService.NewTransferService(nil, tr, wr)

	resp, err := svc.CreateTransfer(context.Background(), &transfer.CreateTransferRequest{
		IdempotencyKey: "key-fail",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         9999,
	})

	assert.NoError(t, err)
	assert.Equal(t, static.StatusFailed, resp.Transfer.Status)
	tr.AssertNotCalled(t, "CreateTransfer")
}

// ─── GetTransfer tests ─────────────────────────────────────────────────────────

func TestGetTransfer_NotFound(t *testing.T) {
	tr := new(MockTransferRepository)
	wr := new(MockWalletRepository)

	tr.On("GetTransferByID", mock.Anything, mock.Anything, "no-such-id").
		Return(nil, nil)

	svc := transferService.NewTransferService(nil, tr, wr)
	_, err := svc.GetTransfer(context.Background(), "no-such-id")

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

func TestGetTransfer_Success(t *testing.T) {
	tr := new(MockTransferRepository)
	wr := new(MockWalletRepository)

	t1 := newProcessedTransfer("txn-1", "wallet_1", "wallet_2", 50)
	ledger := []*transfer.LedgerEntry{
		{ID: "l1", WalletID: "wallet_1", TransferID: "txn-1", Type: static.TypeDebit, Amount: 50},
		{ID: "l2", WalletID: "wallet_2", TransferID: "txn-1", Type: static.TypeCredit, Amount: 50},
	}

	tr.On("GetTransferByID", mock.Anything, mock.Anything, "txn-1").Return(t1, nil)
	tr.On("GetLedgerEntriesByTransferID", mock.Anything, mock.Anything, "txn-1").Return(ledger, nil)

	svc := transferService.NewTransferService(nil, tr, wr)
	resp, err := svc.GetTransfer(context.Background(), "txn-1")

	assert.NoError(t, err)
	assert.Equal(t, "txn-1", resp.Transfer.ID)
	assert.Len(t, resp.Ledger, 2)
	assert.Equal(t, static.TypeDebit, resp.Ledger[0].Type)
	assert.Equal(t, static.TypeCredit, resp.Ledger[1].Type)
}

// ─── Ledger correctness test ───────────────────────────────────────────────────

func TestGetTransfer_LedgerIsBalanced(t *testing.T) {
	tr := new(MockTransferRepository)
	wr := new(MockWalletRepository)

	amount := 250.0
	t1 := newProcessedTransfer("txn-bal", "wallet_1", "wallet_2", amount)
	ledger := []*transfer.LedgerEntry{
		{ID: "l1", WalletID: "wallet_1", TransferID: "txn-bal", Type: static.TypeDebit, Amount: amount},
		{ID: "l2", WalletID: "wallet_2", TransferID: "txn-bal", Type: static.TypeCredit, Amount: amount},
	}

	tr.On("GetTransferByID", mock.Anything, mock.Anything, "txn-bal").Return(t1, nil)
	tr.On("GetLedgerEntriesByTransferID", mock.Anything, mock.Anything, "txn-bal").Return(ledger, nil)

	svc := transferService.NewTransferService(nil, tr, wr)
	resp, err := svc.GetTransfer(context.Background(), "txn-bal")

	assert.NoError(t, err)
	assert.Len(t, resp.Ledger, 2)

	// Verify ledger is balanced: total DEBIT == total CREDIT
	var totalDebit, totalCredit float64
	for _, e := range resp.Ledger {
		if e.Type == static.TypeDebit {
			totalDebit += e.Amount
		} else {
			totalCredit += e.Amount
		}
	}
	assert.Equal(t, totalDebit, totalCredit, "ledger must be balanced")
}
