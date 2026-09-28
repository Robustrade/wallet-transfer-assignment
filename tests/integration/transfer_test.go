package integration

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

func TestCreateTransfer_Success(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 1000, "SGD")
		e.seedWallet("b", 500, "SGD")

		res, err := e.transfer("k1", "a", "b", 300)

		require.NoError(t, err)
		assert.False(t, res.Replayed)
		tr := res.Transfer
		assert.Equal(t, uuid.Version(7), tr.ID.Version())
		assert.Equal(t, "k1", tr.IdempotencyKey)
		assert.Equal(t, "a", tr.FromWalletID)
		assert.Equal(t, "b", tr.ToWalletID)
		assert.Equal(t, int64(300), tr.Amount)
		assert.Equal(t, domain.StatusProcessed, tr.Status)
		assert.Equal(t, domain.FailureReason(""), tr.FailureReason)
		assert.False(t, tr.CreatedAt.IsZero())

		assert.Equal(t, int64(700), e.balance("a"))
		assert.Equal(t, int64(800), e.balance("b"))
		assert.Equal(t, int64(1), e.count("transfers"))
		assert.Equal(t, int64(2), e.count("ledger_entries"))
		assert.Equal(t, int64(1), e.queryInt(
			"SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = 'a' AND entry_type = 'DEBIT' AND amount = 300"))
		assert.Equal(t, int64(1), e.queryInt(
			"SELECT COUNT(*) FROM ledger_entries WHERE wallet_id = 'b' AND entry_type = 'CREDIT' AND amount = 300"))

		// What the service returned is exactly what was stored.
		stored, err := e.store.Repos.Transfers.GetByID(context.Background(), tr.ID)
		require.NoError(t, err)
		assert.Equal(t, tr.Status, stored.Status)
		assert.True(t, tr.CreatedAt.Equal(stored.CreatedAt))
	})
}

func TestCreateTransfer_FullBalanceLeavesZero(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 250, "SGD")
		e.seedWallet("b", 0, "SGD")

		res, err := e.transfer("k1", "a", "b", 250)

		require.NoError(t, err)
		assert.Equal(t, domain.StatusProcessed, res.Transfer.Status)
		assert.Equal(t, int64(0), e.balance("a"))
		assert.Equal(t, int64(250), e.balance("b"))
	})
}

func TestCreateTransfer_BusinessRuleFailuresAreStoredAsFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		from, to struct {
			balance  int64
			currency string
		}
		amount     int64
		wantReason domain.FailureReason
	}{
		{
			name: "insufficient funds",
			from: struct {
				balance  int64
				currency string
			}{100, "SGD"},
			to: struct {
				balance  int64
				currency string
			}{0, "SGD"},
			amount:     101,
			wantReason: domain.ReasonInsufficientFunds,
		},
		{
			name: "currency mismatch",
			from: struct {
				balance  int64
				currency string
			}{100, "SGD"},
			to: struct {
				balance  int64
				currency string
			}{0, "USD"},
			amount:     10,
			wantReason: domain.ReasonCurrencyMismatch,
		},
		{
			name: "destination balance overflow",
			from: struct {
				balance  int64
				currency string
			}{100, "SGD"},
			to: struct {
				balance  int64
				currency string
			}{math.MaxInt64 - 5, "SGD"},
			amount:     10,
			wantReason: domain.ReasonBalanceOverflow,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forEachDriver(t, func(t *testing.T, driver string) {
				e := newEnv(t, driver)
				e.seedWallet("a", tc.from.balance, tc.from.currency)
				e.seedWallet("b", tc.to.balance, tc.to.currency)

				res, err := e.transfer("k1", "a", "b", tc.amount)

				// A business-rule failure is a stored outcome, not an error.
				require.NoError(t, err)
				assert.False(t, res.Replayed)
				assert.Equal(t, domain.StatusFailed, res.Transfer.Status)
				assert.Equal(t, tc.wantReason, res.Transfer.FailureReason)

				assert.Equal(t, tc.from.balance, e.balance("a"), "source unchanged")
				assert.Equal(t, tc.to.balance, e.balance("b"), "destination unchanged")
				assert.Equal(t, int64(1), e.count("transfers"), "FAILED transfer is stored for replay")
				assert.Equal(t, int64(0), e.count("ledger_entries"))
				assert.Equal(t, int64(1), e.count("idempotency_records"))
			})
		})
	}
}

func TestCreateTransfer_WalletNotFoundPersistsNothing(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, from, to string }{
		{"from wallet missing", "ghost", "b"},
		{"to wallet missing", "a", "ghost"},
		{"both wallets missing", "ghost1", "ghost2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			forEachDriver(t, func(t *testing.T, driver string) {
				e := newEnv(t, driver)
				e.seedWallet("a", 100, "SGD")
				e.seedWallet("b", 0, "SGD")

				_, err := e.transfer("k1", tc.from, tc.to, 10)

				require.ErrorIs(t, err, domain.ErrWalletNotFound)
				e.assertNothingPersisted()
				assert.Equal(t, int64(100), e.balance("a"))
			})
		})
	}
}

func TestCreateTransfer_InvalidCommandPersistsNothing(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")

		for _, cmd := range []service.CreateTransferCommand{
			{IdempotencyKey: "", FromWalletID: "a", ToWalletID: "b", Amount: 1},
			{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "a", Amount: 1},
			{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: 0},
			{IdempotencyKey: "k", FromWalletID: "a", ToWalletID: "b", Amount: service.MaxAmount + 1},
		} {
			_, err := e.transferService().CreateTransfer(context.Background(), cmd)
			require.ErrorIs(t, err, domain.ErrValidation, "%+v", cmd)
			var ve *service.ValidationError
			assert.True(t, errors.As(err, &ve))
		}
		e.assertNothingPersisted()
	})
}

func TestWalletService(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 1000, "SGD")
		e.seedWallet("b", 0, "SGD")
		e.seedWallet("c", 0, "SGD")
		ws := e.walletService()
		ctx := context.Background()

		first, err := e.transfer("k1", "a", "b", 10)
		require.NoError(t, err)
		second, err := e.transfer("k2", "b", "a", 5)
		require.NoError(t, err)
		_, err = e.transfer("k3", "a", "c", 1)
		require.NoError(t, err)

		t.Run("GetWallet returns the current balance", func(t *testing.T) {
			w, err := ws.GetWallet(ctx, "b")
			require.NoError(t, err)
			assert.Equal(t, "b", w.ID)
			assert.Equal(t, int64(5), w.Balance)
			assert.Equal(t, "SGD", w.Currency)
			assert.False(t, w.UpdatedAt.IsZero())
		})

		t.Run("GetWallet of a missing wallet", func(t *testing.T) {
			_, err := ws.GetWallet(ctx, "ghost")
			require.ErrorIs(t, err, domain.ErrWalletNotFound)
		})

		t.Run("ListTransfers returns sent and received, newest first", func(t *testing.T) {
			got, err := ws.ListTransfers(ctx, "b", 20)
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, second.Transfer.ID, got[0].ID)
			assert.Equal(t, first.Transfer.ID, got[1].ID)
		})

		t.Run("ListTransfers honors the limit", func(t *testing.T) {
			got, err := ws.ListTransfers(ctx, "a", 2)
			require.NoError(t, err)
			assert.Len(t, got, 2)
		})

		t.Run("ListTransfers of a missing wallet", func(t *testing.T) {
			_, err := ws.ListTransfers(ctx, "ghost", 20)
			require.ErrorIs(t, err, domain.ErrWalletNotFound)
		})

		t.Run("ListTransfers rejects a limit outside 1..100", func(t *testing.T) {
			for _, limit := range []int{0, -1, service.MaxListLimit + 1} {
				_, err := ws.ListTransfers(ctx, "a", limit)
				require.ErrorIs(t, err, domain.ErrValidation, "limit %d", limit)
			}
		})
	})
}
