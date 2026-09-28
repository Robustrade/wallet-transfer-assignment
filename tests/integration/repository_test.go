package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// Repository tests check that each repository method does what its port
// contract (service/ports.go) promises, on every driver. Writes run inside
// a transaction that is rolled back at the end.

var t0 = time.Date(2026, 9, 26, 10, 0, 0, 123456000, time.UTC)

func TestWalletRepository(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 150, "SGD")
		ctx := context.Background()

		t.Run("Get returns the wallet without a transaction", func(t *testing.T) {
			w, err := e.store.Repos.Wallets.Get(ctx, "a")
			require.NoError(t, err)
			assert.Equal(t, "a", w.ID)
			assert.Equal(t, int64(150), w.Balance)
			assert.Equal(t, "SGD", w.Currency)
			assert.False(t, w.UpdatedAt.IsZero())
			assert.Equal(t, time.UTC, w.UpdatedAt.Location())
		})

		t.Run("Get of a missing wallet returns ErrWalletNotFound", func(t *testing.T) {
			_, err := e.store.Repos.Wallets.Get(ctx, "nope")
			require.ErrorIs(t, err, domain.ErrWalletNotFound)
		})

		t.Run("GetForUpdate returns the wallet or ErrWalletNotFound", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				w, err := r.Wallets.GetForUpdate(ctx, "a")
				require.NoError(t, err)
				assert.Equal(t, int64(150), w.Balance)

				_, err = r.Wallets.GetForUpdate(ctx, "nope")
				require.ErrorIs(t, err, domain.ErrWalletNotFound)
			})
		})

		t.Run("UpdateBalance persists the new balance and time", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				require.NoError(t, r.Wallets.UpdateBalance(ctx, "a", 150, 99, t0))
				w, err := r.Wallets.GetForUpdate(ctx, "a")
				require.NoError(t, err)
				assert.Equal(t, int64(99), w.Balance)
				assert.True(t, w.UpdatedAt.Equal(t0), "got %v", w.UpdatedAt)
			})
			assert.Equal(t, int64(150), e.balance("a"), "rolled back")
		})

		t.Run("UpdateBalance of a missing wallet returns ErrWalletNotFound", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				require.ErrorIs(t, r.Wallets.UpdateBalance(ctx, "nope", 0, 1, t0), domain.ErrWalletNotFound)
			})
		})

		t.Run("UpdateBalance refuses to overwrite a balance that changed since it was read", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				// The caller believes the balance is 999, but it is 150. With
				// a plain SET balance = ? this would silently lose an update.
				err := r.Wallets.UpdateBalance(ctx, "a", 999, 5, t0)
				require.ErrorIs(t, err, service.ErrStaleWrite)
				assert.NotErrorIs(t, err, domain.ErrWalletNotFound)

				w, err := r.Wallets.GetForUpdate(ctx, "a")
				require.NoError(t, err)
				assert.Equal(t, int64(150), w.Balance, "balance must be unchanged")
			})
		})
	})
}

func TestTransferRepository(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 1000, "SGD")
		e.seedWallet("b", 1000, "SGD")
		e.seedWallet("c", 1000, "SGD")
		ctx := context.Background()

		t.Run("Create then GetByID round-trips every field exactly", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				tr := newTransfer(t, "k1", "a", "b", 10, t0)
				require.NoError(t, r.Transfers.Create(ctx, tr))

				got, err := r.Transfers.GetByID(ctx, tr.ID)
				require.NoError(t, err)
				assert.Equal(t, tr.ID, got.ID)
				assert.Equal(t, "k1", got.IdempotencyKey)
				assert.Equal(t, "a", got.FromWalletID)
				assert.Equal(t, "b", got.ToWalletID)
				assert.Equal(t, int64(10), got.Amount)
				assert.Equal(t, domain.StatusPending, got.Status)
				assert.Equal(t, domain.FailureReason(""), got.FailureReason)
				// Exact equality, including microseconds, is what makes a
				// replayed createdAt identical to the original.
				assert.True(t, got.CreatedAt.Equal(tr.CreatedAt), "got %v want %v", got.CreatedAt, tr.CreatedAt)
				assert.Equal(t, time.UTC, got.CreatedAt.Location())
			})
		})

		t.Run("GetByID of an unknown id returns ErrNotFound", func(t *testing.T) {
			_, err := e.store.Repos.Transfers.GetByID(ctx, uuid.Must(uuid.NewV7()))
			require.ErrorIs(t, err, service.ErrNotFound)
		})

		t.Run("UpdateStatus persists PROCESSED", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				tr := newTransfer(t, "k1", "a", "b", 10, t0)
				require.NoError(t, r.Transfers.Create(ctx, tr))
				require.NoError(t, tr.MarkProcessed())
				require.NoError(t, r.Transfers.UpdateStatus(ctx, tr, t0.Add(time.Second)))

				got, err := r.Transfers.GetByID(ctx, tr.ID)
				require.NoError(t, err)
				assert.Equal(t, domain.StatusProcessed, got.Status)
				assert.Equal(t, domain.FailureReason(""), got.FailureReason)
			})
		})

		t.Run("UpdateStatus persists FAILED with its reason", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				tr := newTransfer(t, "k1", "a", "b", 10, t0)
				require.NoError(t, r.Transfers.Create(ctx, tr))
				require.NoError(t, tr.MarkFailed(domain.ReasonCurrencyMismatch))
				require.NoError(t, r.Transfers.UpdateStatus(ctx, tr, t0))

				got, err := r.Transfers.GetByID(ctx, tr.ID)
				require.NoError(t, err)
				assert.Equal(t, domain.StatusFailed, got.Status)
				assert.Equal(t, domain.ReasonCurrencyMismatch, got.FailureReason)
			})
		})

		t.Run("guarded UpdateStatus on a PROCESSED transfer returns ErrInvalidTransition and changes nothing", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				tr := newTransfer(t, "k1", "a", "b", 10, t0)
				require.NoError(t, r.Transfers.Create(ctx, tr))
				require.NoError(t, tr.MarkProcessed())
				require.NoError(t, r.Transfers.UpdateStatus(ctx, tr, t0))

				// A stale writer that still believes the transfer is PENDING
				// tries to fail it. The WHERE status='PENDING' guard must stop it.
				stale := *tr
				stale.Status = domain.StatusFailed
				stale.FailureReason = domain.ReasonInsufficientFunds
				err := r.Transfers.UpdateStatus(ctx, &stale, t0)
				require.ErrorIs(t, err, domain.ErrInvalidTransition)

				got, err := r.Transfers.GetByID(ctx, tr.ID)
				require.NoError(t, err)
				assert.Equal(t, domain.StatusProcessed, got.Status)
				assert.Equal(t, domain.FailureReason(""), got.FailureReason)
			})
		})

		t.Run("ListByWallet returns sent and received transfers, newest first, limited", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				older := newTransfer(t, "k1", "a", "b", 1, t0)                  // a sends
				newer := newTransfer(t, "k2", "b", "a", 2, t0.Add(time.Minute)) // a receives
				other := newTransfer(t, "k3", "b", "c", 3, t0.Add(time.Hour))   // a not involved
				// Same timestamp as newer: the UUIDv7 id breaks the tie, and
				// the later-created one sorts first.
				tie := newTransfer(t, "k4", "c", "a", 4, t0.Add(time.Minute))
				for _, tr := range []*domain.Transfer{older, newer, other, tie} {
					require.NoError(t, r.Transfers.Create(ctx, tr))
				}

				got, err := r.Transfers.ListByWallet(ctx, "a", 20)
				require.NoError(t, err)
				require.Len(t, got, 3)
				assert.Equal(t, []uuid.UUID{tie.ID, newer.ID, older.ID},
					[]uuid.UUID{got[0].ID, got[1].ID, got[2].ID})

				got, err = r.Transfers.ListByWallet(ctx, "a", 1)
				require.NoError(t, err)
				require.Len(t, got, 1)
				assert.Equal(t, tie.ID, got[0].ID)
			})
		})

		t.Run("ListByWallet merges sent and received in time order when both exceed the limit", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				// Six transfers alternating direction, one minute apart:
				// even minutes a sends, odd minutes a receives.
				var ids []uuid.UUID
				for i := 0; i < 6; i++ {
					from, to := "a", "b"
					if i%2 == 1 {
						from, to = "b", "a"
					}
					tr := newTransfer(t, fmt.Sprintf("m%d", i), from, to, int64(i+1), t0.Add(time.Duration(i)*time.Minute))
					require.NoError(t, r.Transfers.Create(ctx, tr))
					ids = append(ids, tr.ID)
				}

				// The newest 4 must interleave both directions correctly,
				// which a per-direction LIMIT has to get right.
				got, err := r.Transfers.ListByWallet(ctx, "a", 4)
				require.NoError(t, err)
				require.Len(t, got, 4)
				assert.Equal(t, []uuid.UUID{ids[5], ids[4], ids[3], ids[2]},
					[]uuid.UUID{got[0].ID, got[1].ID, got[2].ID, got[3].ID})
			})
		})

		t.Run("ListByWallet with no transfers returns an empty, non-nil slice", func(t *testing.T) {
			got, err := e.store.Repos.Transfers.ListByWallet(ctx, "c", 20)
			require.NoError(t, err)
			assert.NotNil(t, got)
			assert.Empty(t, got)
		})
	})
}

func TestLedgerRepository(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")
		ctx := context.Background()

		e.inRolledBackTx(func(r service.Repos) {
			tr := newTransfer(t, "k1", "a", "b", 10, t0)
			require.NoError(t, r.Transfers.Create(ctx, tr))
			debit, credit := domain.NewLedgerEntries(tr)

			require.NoError(t, r.Ledger.Insert(ctx, debit, credit))

			// Inserting the same DEBIT again must fail (UNIQUE(transfer_id, entry_type)).
			again, _ := domain.NewLedgerEntries(tr)
			require.Error(t, r.Ledger.Insert(ctx, again))
		})
	})
}

func TestIdempotencyRepository(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100, "SGD")
		e.seedWallet("b", 0, "SGD")
		ctx := context.Background()
		const hash = "2051cec47767b839a8986a5c7d217ca4d604103c78ae86ffb23b8e4763a03336"

		t.Run("Insert claims a new key once", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				inserted, err := r.Idempotency.Insert(ctx, "key-1", hash, t0)
				require.NoError(t, err)
				assert.True(t, inserted)

				// A second insert, even with a different hash, does not
				// overwrite the first.
				otherHash := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
				inserted, err = r.Idempotency.Insert(ctx, "key-1", otherHash, t0)
				require.NoError(t, err)
				assert.False(t, inserted)

				rec, err := r.Idempotency.Get(ctx, "key-1")
				require.NoError(t, err)
				assert.Equal(t, "key-1", rec.Key)
				assert.Equal(t, hash, rec.RequestHash)
				assert.False(t, rec.TransferID.Valid, "not linked yet")
				assert.True(t, rec.CreatedAt.Equal(t0))
			})
		})

		t.Run("keys are case-sensitive", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				for _, key := range []string{"ABC", "abc"} {
					inserted, err := r.Idempotency.Insert(ctx, key, hash, t0)
					require.NoError(t, err)
					assert.True(t, inserted, key)
				}
			})
		})

		t.Run("LinkTransfer links once", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				tr := newTransfer(t, "key-1", "a", "b", 10, t0)
				_, err := r.Idempotency.Insert(ctx, "key-1", hash, t0)
				require.NoError(t, err)
				require.NoError(t, r.Transfers.Create(ctx, tr))

				require.NoError(t, r.Idempotency.LinkTransfer(ctx, "key-1", tr.ID))

				rec, err := r.Idempotency.Get(ctx, "key-1")
				require.NoError(t, err)
				require.True(t, rec.TransferID.Valid)
				assert.Equal(t, tr.ID, rec.TransferID.UUID)

				// Re-linking would point the key at a different outcome.
				// That must never happen.
				require.Error(t, r.Idempotency.LinkTransfer(ctx, "key-1", tr.ID))
			})
		})

		t.Run("LinkTransfer of an unknown key fails", func(t *testing.T) {
			e.inRolledBackTx(func(r service.Repos) {
				tr := newTransfer(t, "k", "a", "b", 10, t0)
				require.NoError(t, r.Transfers.Create(ctx, tr))
				require.Error(t, r.Idempotency.LinkTransfer(ctx, "missing", tr.ID))
			})
		})

		t.Run("Get of an unknown key returns ErrNotFound", func(t *testing.T) {
			_, err := e.store.Repos.Idempotency.Get(ctx, "missing")
			require.ErrorIs(t, err, service.ErrNotFound)
		})
	})
}
