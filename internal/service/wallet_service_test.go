package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type mockWalletRepo struct {
	wallets map[string]domain.Wallet
}

func (m *mockWalletRepo) Get(_ context.Context, id string) (domain.Wallet, error) {
	if w, ok := m.wallets[id]; ok {
		return w, nil
	}
	return domain.Wallet{}, domain.ErrWalletNotFound
}

func (m *mockWalletRepo) LockPair(_ context.Context, firstID, secondID string) (domain.Wallet, domain.Wallet, error) {
	return m.wallets[firstID], m.wallets[secondID], nil
}

func (m *mockWalletRepo) UpdateBalance(_ context.Context, id string, balance domain.Amount) error {
	if w, ok := m.wallets[id]; ok {
		w.Balance = balance
		m.wallets[id] = w
		return nil
	}
	return domain.ErrWalletNotFound
}

type mockTransferRepo struct {
	transfers []domain.Transfer
}

func (m *mockTransferRepo) Create(_ context.Context, t *domain.Transfer) error {
	m.transfers = append(m.transfers, *t)
	return nil
}

func (m *mockTransferRepo) Get(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
	for _, t := range m.transfers {
		if t.ID == id {
			return t, nil
		}
	}
	return domain.Transfer{}, domain.ErrTransferNotFound
}

func (m *mockTransferRepo) ListByWallet(_ context.Context, walletID string, limit int) ([]domain.Transfer, error) {
	var result []domain.Transfer
	for _, t := range m.transfers {
		if t.FromWalletID == walletID || t.ToWalletID == walletID {
			result = append(result, t)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockTransferRepo) UpdateStatus(_ context.Context, id uuid.UUID, from, to domain.TransferStatus, reason *string) error {
	for i := range m.transfers {
		if m.transfers[i].ID == id && m.transfers[i].Status == from {
			m.transfers[i].Status = to
			m.transfers[i].FailureReason = reason
			return nil
		}
	}
	return domain.ErrInvalidStateTransition
}

func TestWalletService_Get(t *testing.T) {
	t.Parallel()

	wallets := &mockWalletRepo{
		wallets: map[string]domain.Wallet{
			"wallet_1": {ID: "wallet_1", Currency: "INR", Balance: 50000},
		},
	}
	transfers := &mockTransferRepo{}
	svc := service.NewWalletService(wallets, transfers)

	w, err := svc.Get(context.Background(), "wallet_1")
	require.NoError(t, err)
	require.Equal(t, "wallet_1", w.ID)
	require.Equal(t, domain.Amount(50000), w.Balance)

	w, err = svc.Get(context.Background(), "  wallet_1  ")
	require.NoError(t, err)
	require.Equal(t, "wallet_1", w.ID)

	_, err = svc.Get(context.Background(), "wallet_unknown")
	require.ErrorIs(t, err, domain.ErrWalletNotFound)

	_, err = svc.Get(context.Background(), "")
	require.ErrorIs(t, err, domain.ErrValidation)
}

func TestWalletService_History(t *testing.T) {
	t.Parallel()

	wallets := &mockWalletRepo{
		wallets: map[string]domain.Wallet{
			"wallet_1": {ID: "wallet_1", Currency: "INR", Balance: 50000},
		},
	}
	transfers := &mockTransferRepo{
		transfers: []domain.Transfer{
			{ID: uuid.New(), FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100},
			{ID: uuid.New(), FromWalletID: "wallet_3", ToWalletID: "wallet_1", Amount: 200},
		},
	}
	svc := service.NewWalletService(wallets, transfers)

	history, err := svc.History(context.Background(), "wallet_1", 10)
	require.NoError(t, err)
	require.Len(t, history, 2)

	_, err = svc.History(context.Background(), "wallet_missing", 10)
	require.ErrorIs(t, err, domain.ErrWalletNotFound)

	_, err = svc.History(context.Background(), "", 10)
	require.ErrorIs(t, err, domain.ErrValidation)
}

type mockLedgerRepo struct {
	entries []domain.LedgerEntry
}

func (m *mockLedgerRepo) CreatePair(_ context.Context, entries [2]domain.LedgerEntry) error {
	m.entries = append(m.entries, entries[0], entries[1])
	return nil
}

func (m *mockLedgerRepo) ListByTransfer(_ context.Context, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	var result []domain.LedgerEntry
	for _, e := range m.entries {
		if e.TransferID == transferID {
			result = append(result, e)
		}
	}
	return result, nil
}

type mockIdempotencyRepo struct {
	records map[string]domain.IdempotencyRecord
}

func (m *mockIdempotencyRepo) Get(_ context.Context, key string) (domain.IdempotencyRecord, error) {
	if r, ok := m.records[key]; ok {
		return r, nil
	}
	return domain.IdempotencyRecord{}, domain.ErrIdempotencyRecordNotFound
}

func (m *mockIdempotencyRepo) Create(_ context.Context, record domain.IdempotencyRecord) error {
	if _, ok := m.records[record.Key]; ok {
		return domain.ErrIdempotencyKeyAlreadyUsed
	}
	m.records[record.Key] = record
	return nil
}

type mockTxManager struct{}

func (m *mockTxManager) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func TestTransferService_Get(t *testing.T) {
	t.Parallel()

	transferID := uuid.New()
	transfers := &mockTransferRepo{
		transfers: []domain.Transfer{
			{ID: transferID, FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 500, Currency: "INR", Status: domain.StatusProcessed},
		},
	}
	ledger := &mockLedgerRepo{
		entries: []domain.LedgerEntry{
			{ID: 1, TransferID: transferID, WalletID: "wallet_1", Type: domain.EntryTypeDebit, Amount: 500, Currency: "INR"},
			{ID: 2, TransferID: transferID, WalletID: "wallet_2", Type: domain.EntryTypeCredit, Amount: 500, Currency: "INR"},
		},
	}
	wallets := &mockWalletRepo{}
	idempotency := &mockIdempotencyRepo{records: make(map[string]domain.IdempotencyRecord)}

	svc := service.NewTransferService(&mockTxManager{}, wallets, transfers, ledger, idempotency)

	detail, err := svc.Get(context.Background(), transferID)
	require.NoError(t, err)
	require.Equal(t, transferID, detail.Transfer.ID)
	require.Len(t, detail.Entries, 2)

	_, err = svc.Get(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrTransferNotFound)
}
