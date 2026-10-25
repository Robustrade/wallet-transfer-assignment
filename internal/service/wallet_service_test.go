package service_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockTxManager mocks transaction execution
type MockTxManager struct {
	mock.Mock
}

func (m *MockTxManager) RunInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	args := m.Called(ctx, mock.Anything)
	if args.Get(0) != nil {
		return args.Error(0)
	}
	return fn(nil)
}

// MockWalletRepo mocks wallet storage
type MockWalletRepo struct {
	mock.Mock
}

func (m *MockWalletRepo) Create(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	return m.Called(ctx, tx, wallet).Error(0)
}

func (m *MockWalletRepo) CreateWallet(ctx context.Context, tx *sql.Tx, wallet *domain.Wallet) error {
	return m.Called(ctx, tx, wallet).Error(0)
}

func (m *MockWalletRepo) GetByID(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepo) GetWalletForUpdate(ctx context.Context, tx *sql.Tx, id string) (*domain.Wallet, error) {
	args := m.Called(ctx, tx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Wallet), args.Error(1)
}

func (m *MockWalletRepo) UpdateBalances(ctx context.Context, tx *sql.Tx, fromID, toID string, amount int64) error {
	return m.Called(ctx, tx, fromID, toID, amount).Error(0)
}

// MockTransferRepo mocks transfer & ledger entry storage
type MockTransferRepo struct {
	mock.Mock
}

func (m *MockTransferRepo) CreateTransferAndEntries(
	ctx context.Context,
	tx *sql.Tx,
	transfer *domain.Transfer,
	debit *domain.LedgerEntry,
	credit *domain.LedgerEntry,
) error {
	return m.Called(ctx, tx, transfer, debit, credit).Error(0)
}

// MockIdempotencyRepo mocks the lease-based idempotency store
type MockIdempotencyRepo struct {
	mock.Mock
}

func (m *MockIdempotencyRepo) Reserve(ctx context.Context, key, requestHash string, ttl time.Duration) (*domain.IdempotencyRecord, error) {
	args := m.Called(ctx, key, requestHash, ttl)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.IdempotencyRecord), args.Error(1)
}

func (m *MockIdempotencyRepo) Resolve(ctx context.Context, tx *sql.Tx, key string, statusCode int, responseBody string) error {
	return m.Called(ctx, tx, key, statusCode, responseBody).Error(0)
}

func (m *MockIdempotencyRepo) Release(ctx context.Context, key string) error {
	return m.Called(ctx, key).Error(0)
}

func (m *MockIdempotencyRepo) ReleaseOrSetFailed(ctx context.Context, tx *sql.Tx, key string) error {
	return m.Called(ctx, tx, key).Error(0)
}

func hashPayload(from, to string, amount int64) string {
	payloadBytes := fmt.Sprintf("%s|%s|%d", from, to, amount)
	hashSum := sha256.Sum256([]byte(payloadBytes))
	return hex.EncodeToString(hashSum[:])
}

// --- TABLE-DRIVEN TESTS ---

func TestWalletService_Transfer_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		req           service.TransferRequest
		setupMocks    func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo)
		expectedErr   error
		validateResp  func(t *testing.T, resp *service.TransferResponse)
	}{
		{
			name: "success: valid transfer between wallets",
			req: service.TransferRequest{
				IdempotencyKey: "key-valid-1",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         500,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w1", "w2", 500)
				iRepo.On("Reserve", ctx, "key-valid-1", expectedHash, 15*time.Second).
					Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)
				txMgr.On("RunInTx", ctx, mock.Anything).Return(nil)

				w1 := &domain.Wallet{ID: "w1", Currency: "USD", Balance: 1000}
				w2 := &domain.Wallet{ID: "w2", Currency: "USD", Balance: 200}
				wRepo.On("GetByID", ctx, mock.Anything, "w1").Return(w1, nil)
				wRepo.On("GetByID", ctx, mock.Anything, "w2").Return(w2, nil)

				tRepo.On("CreateTransferAndEntries", ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return(nil)
				wRepo.On("UpdateBalances", ctx, mock.Anything, "w1", "w2", int64(500)).
					Return(nil)
				iRepo.On("Resolve", ctx, mock.Anything, "key-valid-1", 200, mock.Anything).
					Return(nil)
			},
			expectedErr: nil,
			validateResp: func(t *testing.T, resp *service.TransferResponse) {
				require.NotNil(t, resp)
				assert.NotEmpty(t, resp.TransferID)
				assert.Equal(t, "COMPLETED", resp.Status)
				assert.Equal(t, int64(500), resp.Amount)
				assert.Equal(t, "USD", resp.Currency)
			},
		},
		{
			name: "validation failure: missing idempotency key",
			req: service.TransferRequest{
				IdempotencyKey: "",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks:   func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {},
			expectedErr:  domain.ErrIdempotencyKeyRequired,
			validateResp: nil,
		},
		{
			name: "validation failure: missing fromWalletId",
			req: service.TransferRequest{
				IdempotencyKey: "key-no-from",
				FromWalletID:   "",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks:   func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {},
			expectedErr:  errors.New("fromWalletId and toWalletId must be provided"),
			validateResp: nil,
		},
		{
			name: "validation failure: missing toWalletId",
			req: service.TransferRequest{
				IdempotencyKey: "key-no-to",
				FromWalletID:   "w1",
				ToWalletID:     "",
				Amount:         100,
			},
			setupMocks:   func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {},
			expectedErr:  errors.New("fromWalletId and toWalletId must be provided"),
			validateResp: nil,
		},
		{
			name: "validation failure: self transfer",
			req: service.TransferRequest{
				IdempotencyKey: "key-self",
				FromWalletID:   "w1",
				ToWalletID:     "w1",
				Amount:         100,
			},
			setupMocks:   func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {},
			expectedErr:  domain.ErrSelfTransfer,
			validateResp: nil,
		},
		{
			name: "validation failure: zero amount",
			req: service.TransferRequest{
				IdempotencyKey: "key-zero",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         0,
			},
			setupMocks:   func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {},
			expectedErr:  domain.ErrInvalidAmount,
			validateResp: nil,
		},
		{
			name: "validation failure: negative amount",
			req: service.TransferRequest{
				IdempotencyKey: "key-neg",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         -100,
			},
			setupMocks:   func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {},
			expectedErr:  domain.ErrInvalidAmount,
			validateResp: nil,
		},
		{
			name: "business failure: insufficient funds releases lease",
			req: service.TransferRequest{
				IdempotencyKey: "key-insufficient",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         5000,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w1", "w2", 5000)
				iRepo.On("Reserve", ctx, "key-insufficient", expectedHash, 15*time.Second).
					Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)
				txMgr.On("RunInTx", ctx, mock.Anything).Return(nil)

				w1 := &domain.Wallet{ID: "w1", Currency: "USD", Balance: 500} // Less than 5000
				w2 := &domain.Wallet{ID: "w2", Currency: "USD", Balance: 200}
				wRepo.On("GetByID", ctx, mock.Anything, "w1").Return(w1, nil)
				wRepo.On("GetByID", ctx, mock.Anything, "w2").Return(w2, nil)
				iRepo.On("Release", ctx, "key-insufficient").Return(nil)
			},
			expectedErr:  domain.ErrInsufficientFunds,
			validateResp: nil,
		},
		{
			name: "business failure: currency mismatch releases lease",
			req: service.TransferRequest{
				IdempotencyKey: "key-curr-mismatch",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w1", "w2", 100)
				iRepo.On("Reserve", ctx, "key-curr-mismatch", expectedHash, 15*time.Second).
					Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)
				txMgr.On("RunInTx", ctx, mock.Anything).Return(nil)

				w1 := &domain.Wallet{ID: "w1", Currency: "USD", Balance: 1000}
				w2 := &domain.Wallet{ID: "w2", Currency: "EUR", Balance: 200}
				wRepo.On("GetByID", ctx, mock.Anything, "w1").Return(w1, nil)
				wRepo.On("GetByID", ctx, mock.Anything, "w2").Return(w2, nil)
				iRepo.On("Release", ctx, "key-curr-mismatch").Return(nil)
			},
			expectedErr:  domain.ErrCurrencyMismatch,
			validateResp: nil,
		},
		{
			name: "business failure: source wallet not found",
			req: service.TransferRequest{
				IdempotencyKey: "key-no-source",
				FromWalletID:   "w_missing",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w_missing", "w2", 100)
				iRepo.On("Reserve", ctx, "key-no-source", expectedHash, 15*time.Second).
					Return(&domain.IdempotencyRecord{Status: domain.IdempotencyStatusInProgress}, nil)
				txMgr.On("RunInTx", ctx, mock.Anything).Return(nil)

				wRepo.On("GetByID", ctx, mock.Anything, "w_missing").Return((*domain.Wallet)(nil), domain.ErrWalletNotFound)
				iRepo.On("Release", ctx, "key-no-source").Return(nil)
			},
			expectedErr:  domain.ErrWalletNotFound,
			validateResp: nil,
		},
		{
			name: "idempotency: active concurrent lease returns ErrConcurrentRequest",
			req: service.TransferRequest{
				IdempotencyKey: "key-concurrent",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w1", "w2", 100)
				iRepo.On("Reserve", ctx, "key-concurrent", expectedHash, 15*time.Second).
					Return((*domain.IdempotencyRecord)(nil), domain.ErrConcurrentRequest)
			},
			expectedErr:  domain.ErrConcurrentRequest,
			validateResp: nil,
		},
		{
			name: "idempotency: cached replay returns existing response directly",
			req: service.TransferRequest{
				IdempotencyKey: "key-cached",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w1", "w2", 100)
				cachedBody := `{"transferId":"tx-cached-123","status":"COMPLETED","amount":100,"currency":"USD","createdAt":"2026-10-25T00:00:00Z"}`
				iRepo.On("Reserve", ctx, "key-cached", expectedHash, 15*time.Second).
					Return(&domain.IdempotencyRecord{
						Status:       domain.IdempotencyStatusResolved,
						RequestHash:  expectedHash,
						ResponseBody: cachedBody,
					}, nil)
			},
			expectedErr: nil,
			validateResp: func(t *testing.T, resp *service.TransferResponse) {
				require.NotNil(t, resp)
				assert.Equal(t, "tx-cached-123", resp.TransferID)
				assert.Equal(t, "COMPLETED", resp.Status)
				assert.Equal(t, int64(100), resp.Amount)
				assert.Equal(t, "USD", resp.Currency)
			},
		},
		{
			name: "idempotency: conflicting payload hash returns ErrIdempotencyKeyConflict",
			req: service.TransferRequest{
				IdempotencyKey: "key-conflict",
				FromWalletID:   "w1",
				ToWalletID:     "w2",
				Amount:         100,
			},
			setupMocks: func(txMgr *MockTxManager, wRepo *MockWalletRepo, tRepo *MockTransferRepo, iRepo *MockIdempotencyRepo) {
				expectedHash := hashPayload("w1", "w2", 100)
				iRepo.On("Reserve", ctx, "key-conflict", expectedHash, 15*time.Second).
					Return(&domain.IdempotencyRecord{
						Status:      domain.IdempotencyStatusResolved,
						RequestHash: "unmatched-different-hash",
					}, nil)
			},
			expectedErr:  domain.ErrIdempotencyKeyConflict,
			validateResp: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txMgr := new(MockTxManager)
			wRepo := new(MockWalletRepo)
			tRepo := new(MockTransferRepo)
			iRepo := new(MockIdempotencyRepo)

			tt.setupMocks(txMgr, wRepo, tRepo, iRepo)
			svc := service.NewWalletService(txMgr, wRepo, tRepo, iRepo)

			resp, err := svc.Transfer(ctx, tt.req)

			if tt.expectedErr != nil {
				require.Error(t, err)
				if errors.Is(err, tt.expectedErr) {
					assert.True(t, errors.Is(err, tt.expectedErr))
				} else {
					assert.Equal(t, tt.expectedErr.Error(), err.Error())
				}
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				if tt.validateResp != nil {
					tt.validateResp(t, resp)
				}
			}

			txMgr.AssertExpectations(t)
			wRepo.AssertExpectations(t)
			tRepo.AssertExpectations(t)
			iRepo.AssertExpectations(t)
		})
	}
}

func TestWalletService_CreateWallet_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		userID      string
		currency    string
		setupMock   func(wRepo *MockWalletRepo)
		expectedErr bool
	}{
		{
			name:     "success: valid user and currency",
			userID:   "usr_101",
			currency: "USD",
			setupMock: func(wRepo *MockWalletRepo) {
				wRepo.On("Create", ctx, (*sql.Tx)(nil), mock.MatchedBy(func(w *domain.Wallet) bool {
					return w.UserID == "usr_101" && w.Currency == "USD" && w.Balance == 0
				})).Return(nil)
			},
			expectedErr: false,
		},
		{
			name:        "failure: missing user_id",
			userID:      "",
			currency:    "USD",
			setupMock:   func(wRepo *MockWalletRepo) {},
			expectedErr: true,
		},
		{
			name:        "failure: missing currency",
			userID:      "usr_101",
			currency:    "",
			setupMock:   func(wRepo *MockWalletRepo) {},
			expectedErr: true,
		},
		{
			name:     "failure: repository error",
			userID:   "usr_101",
			currency: "EUR",
			setupMock: func(wRepo *MockWalletRepo) {
				wRepo.On("Create", ctx, (*sql.Tx)(nil), mock.Anything).Return(errors.New("db error"))
			},
			expectedErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wRepo := new(MockWalletRepo)
			tt.setupMock(wRepo)

			svc := service.NewWalletService(nil, wRepo, nil, nil)
			w, err := svc.CreateWallet(ctx, tt.userID, tt.currency)

			if tt.expectedErr {
				assert.Error(t, err)
				assert.Nil(t, w)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, w)
				assert.Equal(t, tt.userID, w.UserID)
				assert.Equal(t, tt.currency, w.Currency)
				assert.Equal(t, int64(0), w.Balance)
			}
			wRepo.AssertExpectations(t)
		})
	}
}

func TestWalletService_GetWallet_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		walletID    string
		setupMock   func(wRepo *MockWalletRepo)
		expectedErr error
	}{
		{
			name:     "success: existing wallet",
			walletID: "w_existing",
			setupMock: func(wRepo *MockWalletRepo) {
				expected := &domain.Wallet{ID: "w_existing", UserID: "u1", Currency: "USD", Balance: 500}
				wRepo.On("GetByID", ctx, (*sql.Tx)(nil), "w_existing").Return(expected, nil)
			},
			expectedErr: nil,
		},
		{
			name:        "failure: empty walletID",
			walletID:    "",
			setupMock:   func(wRepo *MockWalletRepo) {},
			expectedErr: domain.ErrWalletNotFound,
		},
		{
			name:     "failure: wallet not found in repo",
			walletID: "w_missing",
			setupMock: func(wRepo *MockWalletRepo) {
				wRepo.On("GetByID", ctx, (*sql.Tx)(nil), "w_missing").Return((*domain.Wallet)(nil), domain.ErrWalletNotFound)
			},
			expectedErr: domain.ErrWalletNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wRepo := new(MockWalletRepo)
			tt.setupMock(wRepo)

			svc := service.NewWalletService(nil, wRepo, nil, nil)
			w, err := svc.GetWallet(ctx, tt.walletID)

			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr)
				assert.Nil(t, w)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, w)
				assert.Equal(t, tt.walletID, w.ID)
			}
			wRepo.AssertExpectations(t)
		})
	}
}
