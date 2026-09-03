package service_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/dbtest"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func runConcurrently(n int, call func(i int) (*service.TransferResult, error)) ([]*service.TransferResult, []error) {
	results := make([]*service.TransferResult, n)
	errs := make([]error, n)

	var start, done sync.WaitGroup
	start.Add(1)
	for i := range n {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			results[i], errs[i] = call(i)
		}()
	}
	start.Done()
	done.Wait()
	return results, errs
}

func TestConcurrentDebitsCannotOverspend(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 550)
		f.wallet("bob", 0)

		const callers, amount = 10, int64(100)
		results, errs := runConcurrently(callers, func(i int) (*service.TransferResult, error) {
			return f.transfer(fmt.Sprintf("key-%d", i), "alice", "bob", amount)
		})

		var processed, failed int
		for i, err := range errs {
			if err != nil {
				t.Fatalf("caller %d: %v", i, err)
			}
			switch results[i].Transfer.Status {
			case domain.StatusProcessed:
				processed++
			case domain.StatusFailed:
				failed++
			default:
				t.Fatalf("caller %d left transfer in %s", i, results[i].Transfer.Status)
			}
		}

		if processed != 5 || failed != 5 {
			t.Errorf("processed = %d, failed = %d, want 5 and 5: only 5 transfers of %d fit in 550",
				processed, failed, amount)
		}
		if got := f.balance("alice"); got != 50 {
			t.Errorf("alice balance = %d, want 50", got)
		}
		if got := f.balance("bob"); got != 500 {
			t.Errorf("bob balance = %d, want 500", got)
		}
		assertLedgerConsistent(t, db, map[string]int64{"alice": 550, "bob": 0})
	})
}

func TestConcurrentDebitsFromOneWalletToDifferentDestinations(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 100)
		f.wallet("bob", 0)
		f.wallet("carol", 0)

		destinations := []string{"bob", "carol"}
		results, errs := runConcurrently(2, func(i int) (*service.TransferResult, error) {
			return f.transfer(fmt.Sprintf("key-%d", i), "alice", destinations[i], 100)
		})

		var processed int
		for i, err := range errs {
			if err != nil {
				t.Fatalf("caller %d: %v", i, err)
			}
			if results[i].Transfer.Status == domain.StatusProcessed {
				processed++
			}
		}
		if processed != 1 {
			t.Errorf("processed = %d, want 1: alice can only fund one of the two transfers", processed)
		}
		if got := f.balance("alice"); got != 0 {
			t.Errorf("alice balance = %d, want 0", got)
		}
		if got := f.balance("bob") + f.balance("carol"); got != 100 {
			t.Errorf("bob + carol = %d, want 100", got)
		}
		assertLedgerConsistent(t, db, map[string]int64{"alice": 100, "bob": 0, "carol": 0})
	})
}

func TestOpposingTransfersDoNotDeadlock(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 1000)
		f.wallet("bob", 1000)

		const callers = 40
		_, errs := runConcurrently(callers, func(i int) (*service.TransferResult, error) {
			from, to := "alice", "bob"
			if i%2 == 1 {
				from, to = "bob", "alice"
			}
			return f.transfer(fmt.Sprintf("key-%d", i), from, to, 10)
		})

		for i, err := range errs {
			if err != nil {
				t.Fatalf("caller %d failed, which for opposing transfers means a deadlock: %v", i, err)
			}
		}
		if got := f.balance("alice") + f.balance("bob"); got != 2000 {
			t.Errorf("alice + bob = %d, want 2000: money was created or destroyed", got)
		}
		assertLedgerConsistent(t, db, map[string]int64{"alice": 1000, "bob": 1000})
	})
}

func TestConcurrentTransfersConserveMoney(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)

		opening := map[string]int64{}
		wallets := []string{"w0", "w1", "w2", "w3"}
		for _, id := range wallets {
			f.wallet(id, 200)
			opening[id] = 200
		}

		const callers = 32
		_, errs := runConcurrently(callers, func(i int) (*service.TransferResult, error) {
			from := wallets[i%len(wallets)]
			to := wallets[(i/len(wallets)+1+i)%len(wallets)]
			if from == to {
				to = wallets[(i+1)%len(wallets)]
			}
			return f.transfer(fmt.Sprintf("key-%d", i), from, to, 25)
		})

		for i, err := range errs {
			if err != nil {
				t.Fatalf("caller %d: %v", i, err)
			}
		}

		var total int64
		for _, id := range wallets {
			total += f.balance(id)
		}
		if total != 800 {
			t.Errorf("total balance = %d, want 800", total)
		}
		assertLedgerConsistent(t, db, opening)
	})
}
