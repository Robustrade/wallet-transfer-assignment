package service_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type outcome struct {
	res service.CreateTransferResult
	err error
}

// runConcurrently fires all requests at once (released by a shared barrier) and collects outcomes.
func runConcurrently(f *fixture, reqs []domain.TransferRequest) []outcome {
	out := make([]outcome, len(reqs))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := f.svc.CreateTransfer(context.Background(), req)
			out[i] = outcome{res, err}
		}()
	}
	close(start)
	wg.Wait()
	return out
}

func TestConcurrency_ParallelDebitsOfOneWalletNeverDoubleSpend(t *testing.T) {
	tests := []struct {
		name          string
		balance       int64
		amount        int64
		requests      int
		wantProcessed int
	}{
		{"wallet funds 10 of 50 debits", 100, 10, 50, 10},
		{"wallet funds exactly 1 of 30 debits", 10, 10, 30, 1}, // maximal race on the last unit of money
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, map[string]int64{"wallet_1": tc.balance, "wallet_2": 0})
			reqs := make([]domain.TransferRequest, tc.requests)
			for i := range reqs {
				reqs[i] = request(newKey(), "wallet_1", "wallet_2", tc.amount)
			}

			outcomes := runConcurrently(f, reqs)

			var processed, failed int
			for _, o := range outcomes {
				require.NoError(t, o.err)
				switch o.res.Transfer.Status {
				case domain.StatusProcessed:
					processed++
				case domain.StatusFailed:
					require.Equal(t, domain.FailureInsufficientFunds, o.res.Transfer.FailureReason)
					failed++
				default:
					t.Fatalf("unexpected status %s", o.res.Transfer.Status)
				}
			}
			require.Equal(t, tc.wantProcessed, processed)
			require.Equal(t, tc.requests-tc.wantProcessed, failed)
			require.Zero(t, f.balance(t, "wallet_1"))
			require.Equal(t, tc.balance, f.balance(t, "wallet_2"))
			f.requireLedgerConsistent(t)
		})
	}
}

func TestConcurrency_ParallelDuplicatesOfOneKeyExecuteExactlyOnce(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_1": 100, "wallet_2": 0})
	req := request(newKey(), "wallet_1", "wallet_2", 30)
	reqs := make([]domain.TransferRequest, 20)
	for i := range reqs {
		reqs[i] = req
	}

	outcomes := runConcurrently(f, reqs)

	originals := 0
	firstID := outcomes[0].res.Transfer.ID
	for _, o := range outcomes {
		require.NoError(t, o.err)
		require.Equal(t, firstID, o.res.Transfer.ID, "every duplicate sees the same transfer")
		require.Equal(t, domain.StatusProcessed, o.res.Transfer.Status)
		if !o.res.Replayed {
			originals++
		}
	}
	require.Equal(t, 1, originals, "exactly one request executes; the rest replay")
	require.Equal(t, 1, f.count(t, "transfers"))
	require.Equal(t, int64(70), f.balance(t, "wallet_1"))
	f.requireLedgerConsistent(t)
}

func TestConcurrency_OppositeDirectionTransfersDoNotDeadlock(t *testing.T) {
	f := newFixture(t, map[string]int64{"wallet_a": 1000, "wallet_b": 1000})
	var reqs []domain.TransferRequest
	for range 25 {
		reqs = append(reqs,
			request(newKey(), "wallet_a", "wallet_b", 7),
			request(newKey(), "wallet_b", "wallet_a", 7))
	}

	outcomes := runConcurrently(f, reqs)

	for _, o := range outcomes {
		require.NoError(t, o.err, "ordered locking must prevent deadlocks (which would surface as ErrBusy)")
		require.Equal(t, domain.StatusProcessed, o.res.Transfer.Status)
	}
	require.Equal(t, int64(1000), f.balance(t, "wallet_a"))
	require.Equal(t, int64(1000), f.balance(t, "wallet_b"))
	f.requireLedgerConsistent(t)
}

func TestConcurrency_RandomTransferStormConservesMoneyAndKeepsTheLedgerBalanced(t *testing.T) {
	opening := map[string]int64{"w1": 500, "w2": 300, "w3": 100, "w4": 0}
	f := newFixture(t, opening)
	ids := []string{"w1", "w2", "w3", "w4"}
	rng := rand.New(rand.NewPCG(1, 2)) // deterministic so failures are reproducible

	var reqs []domain.TransferRequest
	for i := range 200 {
		from := ids[rng.IntN(len(ids))]
		to := ids[rng.IntN(len(ids))]
		for to == from {
			to = ids[rng.IntN(len(ids))]
		}
		key := fmt.Sprintf("storm-%d", i)
		reqs = append(reqs, request(key, from, to, int64(1+rng.IntN(150))))
		if i%10 == 0 { // sprinkle in duplicates of earlier requests
			reqs = append(reqs, reqs[rng.IntN(len(reqs))])
		}
	}

	outcomes := runConcurrently(f, reqs)

	for _, o := range outcomes {
		require.NoError(t, o.err)
	}
	require.Equal(t, 200, f.count(t, "transfers"), "duplicates must not create transfers")
	f.requireLedgerConsistent(t)
}
