package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
	"github.com/synchone/wallet-transfer-assignment/internal/service"
)

type mockRepo struct {
	beginErr error
}

func (m *mockRepo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return nil, m.beginErr
}

func (m *mockRepo) FindTransferForUpdate(ctx context.Context, tx pgx.Tx, key string) (*domain.Transfer, error) {
	return nil, domain.ErrTransferNotFound
}

func (m *mockRepo) CreateTransfer(ctx context.Context, tx pgx.Tx, input domain.CreateTransferInput) (*domain.Transfer, error) {
	return nil, nil
}

func (m *mockRepo) LockWallets(ctx context.Context, tx pgx.Tx, walletIDs ...string) (map[string]domain.Wallet, error) {
	return nil, nil
}

func (m *mockRepo) UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error {
	return nil
}

func (m *mockRepo) InsertLedgerEntry(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, walletID string, entryType domain.LedgerEntryType, amount int64) (int64, error) {
	return 0, nil
}

func (m *mockRepo) ListLedgerEntries(ctx context.Context, tx pgx.Tx, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	return nil, nil
}

func (m *mockRepo) UpdateTransferStatus(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, status domain.TransferStatus, failureReason *string) error {
	return nil
}

func (m *mockRepo) StoreResponseSnapshot(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, snapshot json.RawMessage) error {
	return nil
}

func TestValidateInput(t *testing.T) {
	svc := service.NewTransferService(&mockRepo{}, nil)

	_, err := svc.CreateTransfer(context.Background(), domain.CreateTransferInput{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrMissingIdempotency))

	_, err = svc.CreateTransfer(context.Background(), domain.CreateTransferInput{
		IdempotencyKey: "k",
		FromWalletID:   "a",
		ToWalletID:     "a",
		Amount:         10,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrSameWallet))
}

func TestTransferResultSnapshotRoundTrip(t *testing.T) {
	original := domain.TransferResult{
		TransferID:     uuid.NewString(),
		Status:         domain.StatusProcessed,
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
		LedgerEntryIDs: []int64{1, 2},
	}

	snapshot, err := original.Snapshot()
	require.NoError(t, err)

	decoded, err := domain.TransferResultFromSnapshot(snapshot)
	require.NoError(t, err)
	assert.Equal(t, original.TransferID, decoded.TransferID)
	assert.Equal(t, original.Status, decoded.Status)
	assert.True(t, decoded.IsReplay)
}
