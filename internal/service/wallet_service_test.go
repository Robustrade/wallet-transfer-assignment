package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/mocks"
	"wallet-transfer-assignment/internal/service"
)

func TestWalletService_CreateWallet(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		balance     int64
		setupMock   func(m *mocks.MockWalletRepository)
		expectedErr error
	}{
		{
			name:        "empty wallet id",
			id:          "",
			balance:     1000,
			setupMock:   func(m *mocks.MockWalletRepository) {},
			expectedErr: service.ErrInvalidWalletID,
		},
		{
			name:        "whitespace only wallet id",
			id:          "   ",
			balance:     1000,
			setupMock:   func(m *mocks.MockWalletRepository) {},
			expectedErr: service.ErrInvalidWalletID,
		},
		{
			name:        "negative balance",
			id:          "wallet_1",
			balance:     -10,
			setupMock:   func(m *mocks.MockWalletRepository) {},
			expectedErr: service.ErrNegativeWalletBalance,
		},
		{
			name:    "repository error propagation",
			id:      "wallet_1",
			balance: 1000,
			setupMock: func(m *mocks.MockWalletRepository) {
				m.EXPECT().
					CreateWallet(gomock.Any(), "wallet_1", int64(1000)).
					Return(nil, errors.New("db failure"))
			},
			expectedErr: errors.New("db failure"),
		},
		{
			name:    "successful creation",
			id:      "wallet_1",
			balance: 1000,
			setupMock: func(m *mocks.MockWalletRepository) {
				m.EXPECT().
					CreateWallet(gomock.Any(), "wallet_1", int64(1000)).
					Return(&domain.Wallet{
						ID:        "wallet_1",
						Balance:   1000,
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
					}, nil)
			},
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockRepo := mocks.NewMockWalletRepository(ctrl)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)

			wallet, err := svc.CreateWallet(context.Background(), tt.id, tt.balance)
			if tt.expectedErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedErr) || err.Error() == tt.expectedErr.Error())
				return
			}

			require.NoError(t, err)
			require.NotNil(t, wallet)
			assert.Equal(t, tt.id, wallet.ID)
			assert.Equal(t, tt.balance, wallet.Balance)
		})
	}
}

func TestWalletService_GetWallet(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		setupMock   func(m *mocks.MockWalletRepository)
		expectedErr error
		expectedRes *domain.Wallet
	}{
		{
			name:        "empty wallet id",
			id:          "",
			setupMock:   func(m *mocks.MockWalletRepository) {},
			expectedErr: service.ErrInvalidWalletID,
		},
		{
			name:        "whitespace only wallet id",
			id:          "   ",
			setupMock:   func(m *mocks.MockWalletRepository) {},
			expectedErr: service.ErrInvalidWalletID,
		},
		{
			name: "wallet not found",
			id:   "unknown_wallet",
			setupMock: func(m *mocks.MockWalletRepository) {
				m.EXPECT().
					GetWallet(gomock.Any(), "unknown_wallet").
					Return(nil, domain.ErrWalletNotFound)
			},
			expectedErr: domain.ErrWalletNotFound,
		},
		{
			name: "successful retrieval",
			id:   "wallet_123",
			setupMock: func(m *mocks.MockWalletRepository) {
				m.EXPECT().
					GetWallet(gomock.Any(), "wallet_123").
					Return(&domain.Wallet{
						ID:        "wallet_123",
						Balance:   5000,
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
					}, nil)
			},
			expectedErr: nil,
			expectedRes: &domain.Wallet{
				ID:      "wallet_123",
				Balance: 5000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockRepo := mocks.NewMockWalletRepository(ctrl)
			tt.setupMock(mockRepo)

			svc := service.NewWalletService(mockRepo)

			wallet, err := svc.GetWallet(context.Background(), tt.id)
			if tt.expectedErr != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedErr) || err.Error() == tt.expectedErr.Error())
				return
			}

			require.NoError(t, err)
			require.NotNil(t, wallet)
			assert.Equal(t, tt.expectedRes.ID, wallet.ID)
			assert.Equal(t, tt.expectedRes.Balance, wallet.Balance)
		})
	}
}
