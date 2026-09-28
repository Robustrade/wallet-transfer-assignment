package integration

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// outcome is the result of one concurrent CreateTransfer call.
type outcome struct {
	res service.TransferResult
	err error
}

// runConcurrently calls fn(i) for i in [0, n) on n goroutines. All goroutines
// block on a shared start barrier and are released at once, so the calls
// really overlap instead of running one after another as they are spawned.
func runConcurrently(n int, fn func(i int) outcome) []outcome {
	results := make([]outcome, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = fn(i)
		}(i)
	}
	close(start)
	wg.Wait()
	return results
}

func TestConcurrency_NoDoubleSpend(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("src", 100, "SGD")
		e.seedWallet("dst", 0, "SGD")
		svc := e.transferService()

		// 50 transfers of 10 from a wallet holding 100: exactly 10 can succeed.
		results := runConcurrently(50, func(i int) outcome {
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: fmt.Sprintf("spend-%d", i), FromWalletID: "src", ToWalletID: "dst", Amount: 10,
			})
			return outcome{res, err}
		})

		var processed, failed int
		for _, o := range results {
			require.NoError(t, o.err)
			switch o.res.Transfer.Status {
			case domain.StatusProcessed:
				processed++
			case domain.StatusFailed:
				failed++
				assert.Equal(t, domain.ReasonInsufficientFunds, o.res.Transfer.FailureReason)
			}
		}
		assert.Equal(t, 10, processed, "PROCESSED")
		assert.Equal(t, 40, failed, "FAILED")
		assert.Equal(t, int64(0), e.balance("src"))
		assert.Equal(t, int64(100), e.balance("dst"))
		assert.Equal(t, int64(20), e.count("ledger_entries"))
	})
}

func TestConcurrency_SameKeySameBody(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 1000, "SGD")
		e.seedWallet("b", 0, "SGD")
		svc := e.transferService()
		cmd := service.CreateTransferCommand{IdempotencyKey: "dup", FromWalletID: "a", ToWalletID: "b", Amount: 100}

		results := runConcurrently(20, func(int) outcome {
			res, err := svc.CreateTransfer(context.Background(), cmd)
			return outcome{res, err}
		})

		var originals int
		ids := map[uuid.UUID]bool{}
		for _, o := range results {
			require.NoError(t, o.err)
			ids[o.res.Transfer.ID] = true
			if !o.res.Replayed {
				originals++
			}
		}
		assert.Len(t, ids, 1, "every caller sees the same transfer id")
		assert.Equal(t, 1, originals, "exactly one caller executed; the rest replayed")
		assert.Equal(t, int64(1), e.count("transfers"))
		assert.Equal(t, int64(900), e.balance("a"), "money moved once")
	})
}

func TestConcurrency_SameKeyDifferentAmounts(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("a", 100000, "SGD")
		e.seedWallet("b", 0, "SGD")
		svc := e.transferService()

		results := runConcurrently(20, func(i int) outcome {
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: "contested", FromWalletID: "a", ToWalletID: "b", Amount: int64(i + 1),
			})
			return outcome{res, err}
		})

		// Whoever commits first owns the key. Everyone else either replays
		// (only possible with the identical amount, i.e. nobody here) or
		// gets IDEMPOTENCY_KEY_REUSED. There is never a second transfer.
		var winners, rejected int
		for _, o := range results {
			switch {
			case o.err == nil:
				winners++
			default:
				require.ErrorIs(t, o.err, domain.ErrIdempotencyKeyReused)
				rejected++
			}
		}
		assert.Equal(t, 1, winners)
		assert.Equal(t, 19, rejected)
		assert.Equal(t, int64(1), e.count("transfers"))
	})
}

func TestConcurrency_OppositeDirectionsDoNotDeadlock(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("A", 10000, "SGD")
		e.seedWallet("B", 10000, "SGD")
		svc := e.transferService()

		// Without a fixed lock order, A->B locks A then waits for B while
		// B->A locks B then waits for A: a classic deadlock. Sorting the
		// ids means both lock A first.
		results := runConcurrently(100, func(i int) outcome {
			from, to := "A", "B"
			if i%2 == 1 {
				from, to = "B", "A"
			}
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: fmt.Sprintf("pingpong-%d", i), FromWalletID: from, ToWalletID: to, Amount: 7,
			})
			return outcome{res, err}
		})

		for i, o := range results {
			require.NoError(t, o.err, "transfer %d", i)
			assert.Equal(t, domain.StatusProcessed, o.res.Transfer.Status)
		}
		// 50 each way, same amount: balances end where they started.
		assert.Equal(t, int64(10000), e.balance("A"))
		assert.Equal(t, int64(10000), e.balance("B"))
		assert.Equal(t, int64(100), e.count("transfers"))
	})
}

func TestConcurrency_StressRandomTransfers(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		const wallets = 10
		for i := 0; i < wallets; i++ {
			e.seedWallet(fmt.Sprintf("w%02d", i), 1000, "SGD")
		}
		svc := e.transferService()

		results := runConcurrently(500, func(i int) outcome {
			from := rand.IntN(wallets)
			to := (from + 1 + rand.IntN(wallets-1)) % wallets // any other wallet
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: fmt.Sprintf("stress-%d", i),
				FromWalletID:   fmt.Sprintf("w%02d", from),
				ToWalletID:     fmt.Sprintf("w%02d", to),
				Amount:         int64(1 + rand.IntN(300)),
			})
			return outcome{res, err}
		})

		var processed int
		for i, o := range results {
			require.NoError(t, o.err, "transfer %d", i)
			if o.res.Transfer.Status == domain.StatusProcessed {
				processed++
			}
		}
		t.Logf("%s: %d/500 processed", driver, processed)
		assert.Equal(t, int64(500), e.count("transfers"))
		// The invariant checker verifies conservation of money and ledger
		// consistency when the test ends.
	})
}

func TestConcurrency_ReadsNeverSeeNegativeBalance(t *testing.T) {
	t.Parallel()
	forEachDriver(t, func(t *testing.T, driver string) {
		e := newEnv(t, driver)
		e.seedWallet("src", 500, "SGD")
		e.seedWallet("dst", 0, "SGD")
		svc := e.transferService()
		ws := e.walletService()

		var reads atomic.Int64
		stop := make(chan struct{})
		var readers sync.WaitGroup
		for r := 0; r < 4; r++ {
			readers.Add(1)
			go func() {
				defer readers.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					for _, id := range []string{"src", "dst"} {
						w, err := ws.GetWallet(context.Background(), id)
						if !assert.NoError(t, err) {
							return
						}
						assert.GreaterOrEqual(t, w.Balance, int64(0), "%s balance", id)
						assert.LessOrEqual(t, w.Balance, int64(500), "%s balance", id)
						reads.Add(1)
					}
				}
			}()
		}

		results := runConcurrently(60, func(i int) outcome {
			res, err := svc.CreateTransfer(context.Background(), service.CreateTransferCommand{
				IdempotencyKey: fmt.Sprintf("r-%d", i), FromWalletID: "src", ToWalletID: "dst", Amount: 10,
			})
			return outcome{res, err}
		})
		close(stop)
		readers.Wait()

		for _, o := range results {
			require.NoError(t, o.err)
		}
		assert.Positive(t, reads.Load())
		assert.Equal(t, int64(0), e.balance("src"))
	})
}
