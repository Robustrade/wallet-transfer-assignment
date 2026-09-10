package unit

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Mock Repositories ---

type MockTxManager struct {
	mock.Mock
}

func (m *MockTxManager) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	// Execute the function with nil tx — mocked repos don't actually use tx.
	return fn(nil)
}

type MockWalletRepo struct {
	mock.Mock
}

func (m *MockWalletRepo) Create(ctx context.Context, tx pgx.Tx, wallet *domain.Wallet) error {
	args := m.Called(ctx, tx, wallet)
	return args.Error(0)
}

func (m *MockWalletRepo) GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepo) GetByIDForUpdate(ctx context.Context, tx pgx.Tx, id string) (*domain.Wallet, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepo) UpdateBalance(ctx context.Context, tx pgx.Tx, id string, newBalance int64) error {
	args := m.Called(ctx, tx, id, newBalance)
	return args.Error(0)
}

type MockTransferRepo struct {
	mock.Mock
}

func (m *MockTransferRepo) Create(ctx context.Context, tx pgx.Tx, transfer *domain.Transfer) (*domain.Transfer, bool, error) {
	args := m.Called(ctx, tx, transfer)
	if args.Get(0) == nil {
		return nil, args.Bool(1), args.Error(2)
	}
	return args.Get(0).(*domain.Transfer), args.Bool(1), args.Error(2)
}

func (m *MockTransferRepo) GetByID(ctx context.Context, tx pgx.Tx, id string) (*domain.Transfer, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transfer), args.Error(1)
}

func (m *MockTransferRepo) GetByIdempotencyKey(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, error) {
	args := m.Called(ctx, tx, key)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Transfer), args.Error(1)
}

func (m *MockTransferRepo) UpdateStatus(ctx context.Context, tx pgx.Tx, id string, status domain.TransferStatus, failureReason string) error {
	args := m.Called(ctx, tx, id, status, failureReason)
	return args.Error(0)
}

type MockLedgerRepo struct {
	mock.Mock
}

func (m *MockLedgerRepo) CreateEntries(ctx context.Context, tx pgx.Tx, entries []*domain.LedgerEntry) error {
	args := m.Called(ctx, tx, entries)
	return args.Error(0)
}

func (m *MockLedgerRepo) GetByTransferID(ctx context.Context, tx pgx.Tx, transferID string) ([]*domain.LedgerEntry, error) {
	args := m.Called(ctx, tx, transferID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.LedgerEntry), args.Error(1)
}

func (m *MockLedgerRepo) GetByWalletID(ctx context.Context, tx pgx.Tx, walletID string, limit, offset int) ([]*domain.LedgerEntry, error) {
	args := m.Called(ctx, tx, walletID, limit, offset)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*domain.LedgerEntry), args.Error(1)
}

// --- Transfer Service Tests ---

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestTransferService_CreateTransfer_HappyPath(t *testing.T) {
	txMgr := &MockTxManager{}
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	ledgerRepo := new(MockLedgerRepo)

	svc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, newTestLogger())

	// Create returns the new transfer (not a duplicate)
	transferRepo.On("Create", mock.Anything, mock.Anything, mock.Anything).
		Return(&domain.Transfer{
			ID:             "t1",
			IdempotencyKey: "key1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         100,
			Status:         domain.TransferStatusPending,
		}, false, nil)

	// Lock wallets in order (wallet_1 < wallet_2, so wallet_1 first)
	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_1").
		Return(&domain.Wallet{ID: "wallet_1", Balance: 1000}, nil)
	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_2").
		Return(&domain.Wallet{ID: "wallet_2", Balance: 500}, nil)

	// Update balances
	walletRepo.On("UpdateBalance", mock.Anything, mock.Anything, "wallet_1", int64(900)).Return(nil)
	walletRepo.On("UpdateBalance", mock.Anything, mock.Anything, "wallet_2", int64(600)).Return(nil)

	// Create ledger entries
	ledgerRepo.On("CreateEntries", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	// Mark processed
	transferRepo.On("UpdateStatus", mock.Anything, mock.Anything, "t1", domain.TransferStatusProcessed, "").Return(nil)

	result, err := svc.CreateTransfer(context.Background(), "key1", "wallet_1", "wallet_2", 100)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "t1", result.Transfer.ID)
	assert.False(t, result.IsDuplicate)
	assert.Len(t, result.LedgerEntries, 2)

	walletRepo.AssertExpectations(t)
	transferRepo.AssertExpectations(t)
	ledgerRepo.AssertExpectations(t)
}

func TestTransferService_CreateTransfer_IdempotentReplay(t *testing.T) {
	txMgr := &MockTxManager{}
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	ledgerRepo := new(MockLedgerRepo)

	svc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, newTestLogger())

	existingTransfer := &domain.Transfer{
		ID:             "t1",
		IdempotencyKey: "key1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
		Status:         domain.TransferStatusProcessed,
	}

	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_1").
		Return(&domain.Wallet{ID: "wallet_1", Balance: 1000}, nil)
	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_2").
		Return(&domain.Wallet{ID: "wallet_2", Balance: 500}, nil)

	// Create returns existing transfer (duplicate)
	transferRepo.On("Create", mock.Anything, mock.Anything, mock.Anything).
		Return(existingTransfer, true, nil)

	// Ledger entries for the existing transfer
	existingEntries := []*domain.LedgerEntry{
		{ID: "e1", TransferID: "t1", WalletID: "wallet_1", EntryType: domain.EntryTypeDebit, Amount: 100},
		{ID: "e2", TransferID: "t1", WalletID: "wallet_2", EntryType: domain.EntryTypeCredit, Amount: 100},
	}
	ledgerRepo.On("GetByTransferID", mock.Anything, mock.Anything, "t1").
		Return(existingEntries, nil)

	result, err := svc.CreateTransfer(context.Background(), "key1", "wallet_1", "wallet_2", 100)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.IsDuplicate)
	assert.Equal(t, existingTransfer.ID, result.Transfer.ID)
	assert.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)
	assert.Len(t, result.LedgerEntries, 2)

	// UpdateBalance should NOT have been called for duplicate requests
	walletRepo.AssertNotCalled(t, "UpdateBalance")
}

func TestTransferService_CreateTransfer_InsufficientFunds(t *testing.T) {
	txMgr := &MockTxManager{}
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	ledgerRepo := new(MockLedgerRepo)

	svc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, newTestLogger())

	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_1").
		Return(&domain.Wallet{ID: "wallet_1", Balance: 50}, nil) // Only 50, need 1000
	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_2").
		Return(&domain.Wallet{ID: "wallet_2", Balance: 500}, nil)

	transferRepo.On("Create", mock.Anything, mock.Anything, mock.Anything).
		Return(&domain.Transfer{
			ID:             "t1",
			IdempotencyKey: "key1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         1000,
			Status:         domain.TransferStatusPending,
		}, false, nil)

	// Transfer should be marked as FAILED
	transferRepo.On("UpdateStatus", mock.Anything, mock.Anything, "t1", domain.TransferStatusFailed, "insufficient funds").Return(nil)

	result, err := svc.CreateTransfer(context.Background(), "key1", "wallet_1", "wallet_2", 1000)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInsufficientFunds))
	assert.NotNil(t, result)
	assert.Equal(t, domain.TransferStatusFailed, result.Transfer.Status)
}

func TestTransferService_CreateTransfer_WalletNotFound(t *testing.T) {
	txMgr := &MockTxManager{}
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	ledgerRepo := new(MockLedgerRepo)

	svc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, newTestLogger())

	// wallet_1 locks fine, but wallet_999 doesn't exist (wallet_1 < wallet_999)
	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_1").
		Return(&domain.Wallet{ID: "wallet_1", Balance: 1000}, nil)
	walletRepo.On("GetByIDForUpdate", mock.Anything, mock.Anything, "wallet_999").
		Return(nil, domain.ErrWalletNotFound)

	result, err := svc.CreateTransfer(context.Background(), "key1", "wallet_1", "wallet_999", 100)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrWalletNotFound))
	assert.Nil(t, result)

	transferRepo.AssertNotCalled(t, "Create")
}

func TestTransferService_CreateTransfer_SameWallet(t *testing.T) {
	txMgr := &MockTxManager{}
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	ledgerRepo := new(MockLedgerRepo)

	svc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, newTestLogger())

	// Domain validation should catch this before hitting DB
	result, err := svc.CreateTransfer(context.Background(), "key1", "wallet_1", "wallet_1", 100)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrSameWallet))
	assert.Nil(t, result)

	// No DB calls should have been made
	transferRepo.AssertNotCalled(t, "Create")
	walletRepo.AssertNotCalled(t, "GetByIDForUpdate")
}

func TestTransferService_CreateTransfer_InvalidAmount(t *testing.T) {
	txMgr := &MockTxManager{}
	walletRepo := new(MockWalletRepo)
	transferRepo := new(MockTransferRepo)
	ledgerRepo := new(MockLedgerRepo)

	svc := service.NewTransferService(txMgr, walletRepo, transferRepo, ledgerRepo, newTestLogger())

	result, err := svc.CreateTransfer(context.Background(), "key1", "wallet_1", "wallet_2", -50)

	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidAmount))
	assert.Nil(t, result)
}
