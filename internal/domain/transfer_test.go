package domain_test

import (
	"strings"
	"testing"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestTransferStatus_CanTransitionTo(t *testing.T) {
	statuses := []domain.TransferStatus{
		domain.TransferStatusPending,
		domain.TransferStatusProcessed,
		domain.TransferStatusFailed,
	}

	allowed := map[[2]domain.TransferStatus]bool{
		{domain.TransferStatusPending, domain.TransferStatusProcessed}: true,
		{domain.TransferStatusPending, domain.TransferStatusFailed}:    true,
	}

	for _, from := range statuses {
		for _, to := range statuses {
			from, to := from, to
			t.Run(string(from)+"->"+string(to), func(t *testing.T) {
				want := allowed[[2]domain.TransferStatus{from, to}]
				require.Equal(t, want, from.CanTransitionTo(to))
			})
		}
	}
}

func TestTransferCommand_Validate(t *testing.T) {
	base := domain.TransferCommand{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		AmountMinor:    100,
	}

	t.Run("valid command passes", func(t *testing.T) {
		require.NoError(t, base.Validate())
	})

	cases := []struct {
		name    string
		mutate  func(c domain.TransferCommand) domain.TransferCommand
		wantErr string
	}{
		{"empty key", func(c domain.TransferCommand) domain.TransferCommand { c.IdempotencyKey = ""; return c }, "idempotencyKey"},
		{"key too long", func(c domain.TransferCommand) domain.TransferCommand {
			c.IdempotencyKey = strings.Repeat("a", 256)
			return c
		}, "idempotencyKey"},
		{"non-ascii key", func(c domain.TransferCommand) domain.TransferCommand { c.IdempotencyKey = "café"; return c }, "idempotencyKey"},
		{"invalid from id", func(c domain.TransferCommand) domain.TransferCommand { c.FromWalletID = "bad id!"; return c }, "fromWalletId"},
		{"invalid to id", func(c domain.TransferCommand) domain.TransferCommand { c.ToWalletID = "bad id!"; return c }, "toWalletId"},
		{"same wallet twice", func(c domain.TransferCommand) domain.TransferCommand { c.ToWalletID = c.FromWalletID; return c }, "toWalletId"},
		{"zero amount", func(c domain.TransferCommand) domain.TransferCommand { c.AmountMinor = 0; return c }, "amount"},
		{"negative amount", func(c domain.TransferCommand) domain.TransferCommand { c.AmountMinor = -5; return c }, "amount"},
		{"amount too large", func(c domain.TransferCommand) domain.TransferCommand {
			c.AmountMinor = domain.MaxAmountMinor + 1
			return c
		}, "amount"},
		{"invalid currency", func(c domain.TransferCommand) domain.TransferCommand { c.Currency = "usd"; return c }, "currency"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mutate(base).Validate()
			require.Error(t, err)
			var ve *domain.ValidationError
			require.ErrorAs(t, err, &ve)
			require.Equal(t, tc.wantErr, ve.Field)
		})
	}
}

func TestTransferCommand_Fingerprint(t *testing.T) {
	a := domain.TransferCommand{FromWalletID: "w1", ToWalletID: "w2", AmountMinor: 100, Currency: "INR"}
	b := domain.TransferCommand{FromWalletID: "w1", ToWalletID: "w2", AmountMinor: 100, Currency: "INR"}
	require.Equal(t, a.Fingerprint(), b.Fingerprint(), "identical commands must fingerprint identically")

	variants := []domain.TransferCommand{
		{FromWalletID: "w9", ToWalletID: "w2", AmountMinor: 100, Currency: "INR"},
		{FromWalletID: "w1", ToWalletID: "w9", AmountMinor: 100, Currency: "INR"},
		{FromWalletID: "w1", ToWalletID: "w2", AmountMinor: 101, Currency: "INR"},
		{FromWalletID: "w1", ToWalletID: "w2", AmountMinor: 100, Currency: "USD"},
		{FromWalletID: "w1", ToWalletID: "w2", AmountMinor: 100, Currency: ""},
	}
	for _, v := range variants {
		require.NotEqual(t, a.Fingerprint(), v.Fingerprint(), "%+v should not collide with base", v)
	}
}

func TestWallet_CanDebit(t *testing.T) {
	user := domain.Wallet{Kind: domain.WalletKindUser, BalanceMinor: 100}
	require.True(t, user.CanDebit(100))
	require.True(t, user.CanDebit(50))
	require.False(t, user.CanDebit(101))

	system := domain.Wallet{Kind: domain.WalletKindSystem, BalanceMinor: 0}
	require.True(t, system.CanDebit(1_000_000))
}

func TestNewWallet(t *testing.T) {
	w, err := domain.NewWallet("wallet_1", "INR")
	require.NoError(t, err)
	require.Equal(t, "wallet_1", w.ID)
	require.Equal(t, domain.WalletKindUser, w.Kind)
	require.Equal(t, "INR", w.Currency)
	require.Zero(t, w.BalanceMinor)

	_, err = domain.NewWallet("bad id!", "INR")
	require.Error(t, err)

	_, err = domain.NewWallet("wallet_1", "inr")
	require.Error(t, err)
}
