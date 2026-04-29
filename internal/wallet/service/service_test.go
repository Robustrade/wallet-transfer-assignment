package service_test

import (
	"context"
	"errors"
	"testing"

	"wallet-transfer/internal/wallet"
	walletService "wallet-transfer/internal/wallet/service"
	"wallet-transfer/internal/wallet/static"
	"wallet-transfer/pkg/apperror"
	"wallet-transfer/pkg/database"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ─── Mock ─────────────────────────────────────────────────────────────────────

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

// ─── CreateWallet ─────────────────────────────────────────────────────────────

func TestCreateWallet_Success(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	w, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerName:      "Alice",
		OwnerEmail:     "alice@example.com",
		InitialBalance: 500,
	})

	assert.NoError(t, err)
	assert.NotEmpty(t, w.ID)
	assert.Equal(t, "Alice", w.OwnerName)
	assert.Equal(t, "alice@example.com", w.OwnerEmail)
	assert.Equal(t, 500.0, w.Balance)
}

func TestCreateWallet_MissingName(t *testing.T) {
	svc := walletService.NewWalletService(nil, new(MockWalletRepository))

	_, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerEmail:     "alice@example.com",
		InitialBalance: 100,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestCreateWallet_MissingEmail(t *testing.T) {
	svc := walletService.NewWalletService(nil, new(MockWalletRepository))

	_, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerName:      "Alice",
		InitialBalance: 100,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestCreateWallet_NegativeBalance(t *testing.T) {
	svc := walletService.NewWalletService(nil, new(MockWalletRepository))

	_, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerName:      "Alice",
		OwnerEmail:     "alice@example.com",
		InitialBalance: -10,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

func TestCreateWallet_AlreadyExists(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("Create", mock.Anything, mock.Anything, mock.Anything).
		Return(&pgconn.PgError{Code: "23505"})

	_, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerName:      "Alice",
		OwnerEmail:     "alice@example.com",
		InitialBalance: 100,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409, appErr.Code)
}

func TestCreateWallet_RepoError(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("Create", mock.Anything, mock.Anything, mock.Anything).Return(errors.New("db error"))

	_, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerName:      "Alice",
		OwnerEmail:     "alice@example.com",
		InitialBalance: 0,
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 500, appErr.Code)
}

func TestCreateWallet_InvalidEmail(t *testing.T) {
	svc := walletService.NewWalletService(nil, new(MockWalletRepository))

	_, err := svc.CreateWallet(context.Background(), &wallet.CreateWalletRequest{
		OwnerName:  "Alice",
		OwnerEmail: "not-an-email",
	})

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400, appErr.Code)
}

// ─── GetWallet ────────────────────────────────────────────────────────────────

func TestGetWallet_Success(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	expected := &wallet.Wallet{ID: "wallet_1", Balance: 250}
	repo.On("GetByID", mock.Anything, mock.Anything, "wallet_1").Return(expected, nil)

	w, err := svc.GetWallet(context.Background(), "wallet_1")

	assert.NoError(t, err)
	assert.Equal(t, "wallet_1", w.ID)
	assert.Equal(t, 250.0, w.Balance)
}

func TestGetWallet_NotFound(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("GetByID", mock.Anything, mock.Anything, "ghost").Return(nil, nil)

	_, err := svc.GetWallet(context.Background(), "ghost")

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
	assert.Equal(t, static.WalletNotFound, appErr.Message)
}

func TestGetWallet_RepoError(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("GetByID", mock.Anything, mock.Anything, "wallet_1").
		Return(nil, errors.New("db error"))

	_, err := svc.GetWallet(context.Background(), "wallet_1")

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 500, appErr.Code)
}

// ─── GetBalance ───────────────────────────────────────────────────────────────

func TestGetBalance_Success(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("GetByID", mock.Anything, mock.Anything, "wallet_1").Return(&wallet.Wallet{ID: "wallet_1", Balance: 750}, nil)

	bal, err := svc.GetBalance(context.Background(), "wallet_1")

	assert.NoError(t, err)
	assert.Equal(t, "wallet_1", bal.WalletID)
	assert.Equal(t, 750.0, bal.Balance)
}

func TestGetBalance_WalletNotFound(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("GetByID", mock.Anything, mock.Anything, "ghost").Return(nil, nil)

	_, err := svc.GetBalance(context.Background(), "ghost")

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 404, appErr.Code)
}

// ─── GetAllWallets ────────────────────────────────────────────────────────────

func TestGetAllWallets_Success(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	wallets := []*wallet.Wallet{
		{ID: "wallet_1", Balance: 100},
		{ID: "wallet_2", Balance: 200},
	}
	repo.On("GetAll", mock.Anything, mock.Anything).Return(wallets, nil)

	result, err := svc.GetAllWallets(context.Background())

	assert.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestGetAllWallets_RepoError(t *testing.T) {
	repo := new(MockWalletRepository)
	svc := walletService.NewWalletService(nil, repo)

	repo.On("GetAll", mock.Anything, mock.Anything).Return(nil, errors.New("db error"))

	_, err := svc.GetAllWallets(context.Background())

	assert.Error(t, err)
	var appErr *apperror.AppError
	assert.True(t, errors.As(err, &appErr))
	assert.Equal(t, 500, appErr.Code)
}
