package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
	"wallet-transfer/internal/tests/mock"
)

func validParams() domain.NewTransferParams {
	return domain.NewTransferParams{
		IdempotencyKey: "k", FromWalletID: uuid.New(), ToWalletID: uuid.New(), Amount: dec("10"),
	}
}

func TestCreateTransfer_ValidationFails(t *testing.T) {
	store := &mock.Store{EnqueueFn: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		t.Fatal("store must not be called on validation failure")
		return domain.Transfer{}, false, nil
	}}
	rec := &mock.Recorder{}
	svc := service.NewTransferService(store, zap.NewNop(), rec)

	p := validParams()
	p.IdempotencyKey = ""
	_, _, err := svc.CreateTransfer(context.Background(), p)
	require.ErrorIs(t, err, domain.ErrEmptyIdempotencyKey)
	require.Equal(t, 0, rec.Enqueued)
}

func TestCreateTransfer_NewIncrementsMetric(t *testing.T) {
	store := &mock.Store{EnqueueFn: func(_ context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error) {
		return domain.Transfer{ID: uuid.New(), IdempotencyKey: p.IdempotencyKey}, true, nil
	}}
	rec := &mock.Recorder{}
	svc := service.NewTransferService(store, zap.NewNop(), rec)

	_, created, err := svc.CreateTransfer(context.Background(), validParams())
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, 1, rec.Enqueued)
}

func TestCreateTransfer_ReplayDoesNotIncrement(t *testing.T) {
	store := &mock.Store{EnqueueFn: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return domain.Transfer{ID: uuid.New()}, false, nil // existing
	}}
	rec := &mock.Recorder{}
	svc := service.NewTransferService(store, zap.NewNop(), rec)

	_, created, err := svc.CreateTransfer(context.Background(), validParams())
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, 0, rec.Enqueued)
}

func TestCreateTransfer_StoreErrorPropagates(t *testing.T) {
	store := &mock.Store{EnqueueFn: func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error) {
		return domain.Transfer{}, false, errors.New("db down")
	}}
	rec := &mock.Recorder{}
	svc := service.NewTransferService(store, zap.NewNop(), rec)

	_, _, err := svc.CreateTransfer(context.Background(), validParams())
	require.Error(t, err)
	require.Equal(t, 0, rec.Enqueued)
}

func TestGetTransfer_Delegates(t *testing.T) {
	want := domain.Transfer{ID: uuid.New(), State: domain.StateProcessed}
	store := &mock.Store{GetFn: func(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
		require.Equal(t, want.ID, id)
		return want, nil
	}}
	svc := service.NewTransferService(store, zap.NewNop(), &mock.Recorder{})

	got, err := svc.GetTransfer(context.Background(), want.ID)
	require.NoError(t, err)
	require.Equal(t, want.State, got.State)
}
