package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

var (
	t0 = time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Second)
)

func validRequest() domain.TransferRequest {
	return domain.TransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
}

func TestTransferRequest_Validate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(r *domain.TransferRequest)
		wantField string
	}{
		{"valid request", func(*domain.TransferRequest) {}, ""},
		{"missing idempotency key", func(r *domain.TransferRequest) { r.IdempotencyKey = "" }, "idempotencyKey"},
		{"blank idempotency key", func(r *domain.TransferRequest) { r.IdempotencyKey = "   " }, "idempotencyKey"},
		{"idempotency key too long", func(r *domain.TransferRequest) { r.IdempotencyKey = strings.Repeat("k", 256) }, "idempotencyKey"},
		{"idempotency key at max length", func(r *domain.TransferRequest) { r.IdempotencyKey = strings.Repeat("k", 255) }, ""},
		{"missing source wallet", func(r *domain.TransferRequest) { r.FromWalletID = "" }, "fromWalletId"},
		{"missing destination wallet", func(r *domain.TransferRequest) { r.ToWalletID = "" }, "toWalletId"},
		{"wallet id too long", func(r *domain.TransferRequest) { r.ToWalletID = strings.Repeat("w", 65) }, "toWalletId"},
		{"same source and destination", func(r *domain.TransferRequest) { r.ToWalletID = r.FromWalletID }, "toWalletId"},
		{"zero amount", func(r *domain.TransferRequest) { r.Amount = 0 }, "amount"},
		{"negative amount", func(r *domain.TransferRequest) { r.Amount = -5 }, "amount"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			tc.mutate(&req)

			err := req.Validate()

			if tc.wantField == "" {
				require.NoError(t, err)
				return
			}
			var verr *domain.ValidationError
			require.True(t, errors.As(err, &verr), "expected ValidationError, got %v", err)
			require.Equal(t, tc.wantField, verr.Field)
		})
	}
}

func TestNewPendingTransfer_StartsPendingWithoutLedgerEntries(t *testing.T) {
	id := uuid.New()

	tr, err := domain.NewPendingTransfer(id, validRequest(), t0)

	require.NoError(t, err)
	require.Equal(t, id, tr.ID)
	require.Equal(t, domain.StatusPending, tr.Status)
	require.Empty(t, tr.FailureReason)
	require.Equal(t, t0, tr.CreatedAt)
	_, err = tr.LedgerEntries()
	require.ErrorIs(t, err, domain.ErrInvalidTransition, "only processed transfers have ledger entries")
}

func TestNewPendingTransfer_RejectsInvalidRequest(t *testing.T) {
	req := validRequest()
	req.Amount = 0

	_, err := domain.NewPendingTransfer(uuid.New(), req, t0)

	var verr *domain.ValidationError
	require.True(t, errors.As(err, &verr))
}

func TestExecute_ProcessesWhenFundsSuffice(t *testing.T) {
	for _, balance := range []int64{100, 1000} { // exact balance and surplus
		tr := pendingTransfer(t)

		err := tr.Execute(domain.Wallet{ID: "wallet_1", Balance: balance}, t1)

		require.NoError(t, err)
		require.Equal(t, domain.StatusProcessed, tr.Status)
		require.Empty(t, tr.FailureReason)
		require.Equal(t, t1, tr.UpdatedAt)
	}
}

func TestExecute_FailsWithReasonWhenFundsAreInsufficient(t *testing.T) {
	tr := pendingTransfer(t)

	err := tr.Execute(domain.Wallet{ID: "wallet_1", Balance: 99}, t1)

	require.NoError(t, err, "insufficient funds is a business outcome, not an error")
	require.Equal(t, domain.StatusFailed, tr.Status)
	require.Equal(t, domain.FailureInsufficientFunds, tr.FailureReason)
	_, err = tr.LedgerEntries()
	require.ErrorIs(t, err, domain.ErrInvalidTransition, "failed transfers must not produce ledger entries")
}

func TestExecute_RejectsWalletThatIsNotTheSource(t *testing.T) {
	tr := pendingTransfer(t)

	err := tr.Execute(domain.Wallet{ID: "wallet_2", Balance: 1000}, t1)

	require.Error(t, err)
	require.Equal(t, domain.StatusPending, tr.Status)
}

func TestTerminalTransfersCannotTransitionAgain(t *testing.T) {
	processed := pendingTransfer(t)
	require.NoError(t, processed.Execute(domain.Wallet{ID: "wallet_1", Balance: 100}, t1))
	failed := pendingTransfer(t)
	require.NoError(t, failed.Execute(domain.Wallet{ID: "wallet_1", Balance: 0}, t1))

	for _, tr := range []domain.Transfer{processed, failed} {
		before := tr

		require.ErrorIs(t, tr.Execute(domain.Wallet{ID: "wallet_1", Balance: 100}, t1), domain.ErrInvalidTransition)
		require.ErrorIs(t, tr.Process(t1), domain.ErrInvalidTransition)
		require.ErrorIs(t, tr.Fail(domain.FailureInsufficientFunds, t1), domain.ErrInvalidTransition)
		require.Equal(t, before, tr, "a rejected transition must not change the transfer")
	}
}

func TestFail_RequiresReason(t *testing.T) {
	tr := pendingTransfer(t)

	require.Error(t, tr.Fail("", t1))
	require.Equal(t, domain.StatusPending, tr.Status)
}

func TestLedgerEntries_AreABalancedDebitCreditPair(t *testing.T) {
	tr := pendingTransfer(t)
	require.NoError(t, tr.Execute(domain.Wallet{ID: "wallet_1", Balance: 100}, t1))

	entries, err := tr.LedgerEntries()

	require.NoError(t, err)
	require.Equal(t, []domain.LedgerEntry{
		{TransferID: tr.ID, WalletID: "wallet_1", Type: domain.EntryDebit, Amount: 100},
		{TransferID: tr.ID, WalletID: "wallet_2", Type: domain.EntryCredit, Amount: 100},
	}, entries)
}

func pendingTransfer(t *testing.T) domain.Transfer {
	t.Helper()
	tr, err := domain.NewPendingTransfer(uuid.New(), validRequest(), t0)
	require.NoError(t, err)
	return tr
}
