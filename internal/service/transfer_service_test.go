package service_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
	"wallet-transfer-assignment/pkg/testutil"
)

func setupServicesWithRepos(t *testing.T) (*service.TransferService, *service.WalletService, repository.Repositories) {
	t.Helper()
	transferSvc, walletSvc, _, repos := setupServicesWithTxManager(t)
	return transferSvc, walletSvc, repos
}

func setupServicesWithTxManager(t *testing.T) (*service.TransferService, *service.WalletService, repository.TxManager, repository.Repositories) {
	t.Helper()
	pool := testutil.SetupTestDB(t)

	repos := repository.Repositories{
		Wallets:     postgres.NewWalletRepository(pool),
		Transfers:   postgres.NewTransferRepository(pool),
		Ledger:      postgres.NewLedgerRepository(pool),
		Idempotency: postgres.NewIdempotencyRepository(pool),
	}
	txManager := postgres.NewTxManager(pool)

	transferService := service.NewTransferService(txManager, repos)
	walletService := service.NewWalletService(txManager, repos)

	return transferService, walletService, txManager, repos
}

func setupServicesWithPool(t *testing.T) (*service.TransferService, *service.WalletService, *pgxpool.Pool, repository.Repositories) {
	t.Helper()
	pool := testutil.SetupTestDB(t)

	repos := repository.Repositories{
		Wallets:     postgres.NewWalletRepository(pool),
		Transfers:   postgres.NewTransferRepository(pool),
		Ledger:      postgres.NewLedgerRepository(pool),
		Idempotency: postgres.NewIdempotencyRepository(pool),
	}
	txManager := postgres.NewTxManager(pool)

	transferService := service.NewTransferService(txManager, repos)
	walletService := service.NewWalletService(txManager, repos)

	return transferService, walletService, pool, repos
}

type failingLedgerTxManager struct {
	realTxManager repository.TxManager
}

type failingLedgerRepo struct {
	repository.LedgerRepository
}

func (f *failingLedgerRepo) CreateLedgerEntries(ctx context.Context, entries ...domain.LedgerEntry) error {
	return errors.New("simulated ledger persistence failure after wallet insertion")
}

func (m *failingLedgerTxManager) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	return m.realTxManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		txRepos.Ledger = &failingLedgerRepo{LedgerRepository: txRepos.Ledger}
		return fn(txRepos)
	})
}

func setupServices(t *testing.T) (*service.TransferService, *service.WalletService) {
	transferSvc, walletSvc, _ := setupServicesWithRepos(t)
	return transferSvc, walletSvc
}

func TestTransferService_Success(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc := setupServices(t)

	// 1. Seed two wallets
	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_1",
		Name:           "Alice",
		InitialBalance: 500,
	})
	if err != nil {
		t.Fatalf("failed to create wallet 1: %v", err)
	}

	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_2",
		Name:           "Bob",
		InitialBalance: 200,
	})
	if err != nil {
		t.Fatalf("failed to create wallet 2: %v", err)
	}

	// 2. Execute transfer of 150
	req := service.CreateTransferRequest{
		IdempotencyKey: "key_tx_1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         150,
	}

	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("unexpected transfer error: %v", err)
	}

	if resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s", resp.Status)
	}
	if resp.Amount != 150 {
		t.Fatalf("expected amount 150, got %d", resp.Amount)
	}

	// 3. Verify updated balances
	updatedW1, err := walletSvc.GetWallet(ctx, w1.ID)
	if err != nil {
		t.Fatalf("failed to get wallet 1: %v", err)
	}
	if updatedW1.Balance != 350 {
		t.Fatalf("expected wallet 1 balance 350, got %d", updatedW1.Balance)
	}

	updatedW2, err := walletSvc.GetWallet(ctx, w2.ID)
	if err != nil {
		t.Fatalf("failed to get wallet 2: %v", err)
	}
	if updatedW2.Balance != 350 {
		t.Fatalf("expected wallet 2 balance 350, got %d", updatedW2.Balance)
	}

	// 4. Verify double-entry ledger entries
	ledgerW1, err := walletSvc.GetWalletLedger(ctx, w1.ID, 50, 0)
	if err != nil {
		t.Fatalf("failed to get wallet 1 ledger: %v", err)
	}
	// Initial deposit (credit 500) + transfer debit (debit 150) = 2 entries
	if len(ledgerW1) != 2 {
		t.Fatalf("expected 2 ledger entries for wallet 1, got %d", len(ledgerW1))
	}
	if ledgerW1[1].Type != domain.LedgerEntryTypeDebit || ledgerW1[1].Amount != 150 {
		t.Fatalf("expected debit entry of 150, got %+v", ledgerW1[1])
	}

	ledgerW2, err := walletSvc.GetWalletLedger(ctx, w2.ID, 50, 0)
	if err != nil {
		t.Fatalf("failed to get wallet 2 ledger: %v", err)
	}
	if len(ledgerW2) != 2 {
		t.Fatalf("expected 2 ledger entries for wallet 2, got %d", len(ledgerW2))
	}
	if ledgerW2[1].Type != domain.LedgerEntryTypeCredit || ledgerW2[1].Amount != 150 {
		t.Fatalf("expected credit entry of 150, got %+v", ledgerW2[1])
	}

	// 5. Verify ledger reconciliation
	storedBal1, ledgerBal1, isBalanced1, err := walletSvc.ReconcileBalance(ctx, w1.ID)
	if err != nil || !isBalanced1 {
		t.Fatalf("wallet 1 ledger reconciliation failed: stored=%d, ledger=%d, balanced=%v, err=%v", storedBal1, ledgerBal1, isBalanced1, err)
	}

	storedBal2, ledgerBal2, isBalanced2, err := walletSvc.ReconcileBalance(ctx, w2.ID)
	if err != nil || !isBalanced2 {
		t.Fatalf("wallet 2 ledger reconciliation failed: stored=%d, ledger=%d, balanced=%v, err=%v", storedBal2, ledgerBal2, isBalanced2, err)
	}
}

func TestTransferService_InsufficientFunds(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc := setupServices(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_broke",
		Name:           "Poor Charlie",
		InitialBalance: 50,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_dest",
		Name:           "Recipient",
		InitialBalance: 100,
	})

	req := service.CreateTransferRequest{
		IdempotencyKey: "key_fail_1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100, // Attempting to transfer 100 with only 50
	}

	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	if resp == nil || resp.Status != domain.TransferStatusFailed {
		t.Fatalf("expected FAILED response status, got %+v", resp)
	}

	// Balances must remain unchanged
	updatedW1, _ := walletSvc.GetWallet(ctx, w1.ID)
	if updatedW1.Balance != 50 {
		t.Fatalf("expected wallet 1 balance unchanged (50), got %d", updatedW1.Balance)
	}
	updatedW2, _ := walletSvc.GetWallet(ctx, w2.ID)
	if updatedW2.Balance != 100 {
		t.Fatalf("expected wallet 2 balance unchanged (100), got %d", updatedW2.Balance)
	}

	// Replaying with identical key must return cached failed response
	replayResp, replayErr := transferSvc.ExecuteTransfer(ctx, req)
	if !errors.Is(replayErr, domain.ErrInsufficientFunds) {
		t.Fatalf("expected replayed ErrInsufficientFunds, got %v", replayErr)
	}
	if !replayResp.IsReplay {
		t.Fatalf("expected IsReplay = true on duplicate request")
	}
	if replayResp.TransferID != resp.TransferID {
		t.Fatalf("expected identical transfer ID on replay, got %s vs %s", replayResp.TransferID, resp.TransferID)
	}
}

func TestTransferService_IdempotencyReplay(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc := setupServices(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_idem_from",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_idem_to",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	req := service.CreateTransferRequest{
		IdempotencyKey: "unique_idem_key_42",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         250,
	}

	// First execution
	resp1, err := transferSvc.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}
	if resp1.IsReplay {
		t.Fatalf("first response should not be marked as replay")
	}

	// Second execution with identical key and payload
	resp2, err := transferSvc.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("replay transfer failed: %v", err)
	}
	if !resp2.IsReplay {
		t.Fatalf("second response must be marked as replay")
	}
	if resp2.TransferID != resp1.TransferID {
		t.Fatalf("expected same transfer ID, got %s and %s", resp1.TransferID, resp2.TransferID)
	}
	if resp1.ResponseCode != 201 {
		t.Fatalf("expected first response code 201, got %d", resp1.ResponseCode)
	}
	if resp2.ResponseCode != 201 {
		t.Fatalf("expected replayed response code 201, got %d", resp2.ResponseCode)
	}

	// Verify balance was only deducted ONCE
	w1After, _ := walletSvc.GetWallet(ctx, w1.ID)
	if w1After.Balance != 750 {
		t.Fatalf("expected balance 750 (single debit), got %d", w1After.Balance)
	}
	w2After, _ := walletSvc.GetWallet(ctx, w2.ID)
	if w2After.Balance != 250 {
		t.Fatalf("expected balance 250 (single credit), got %d", w2After.Balance)
	}
}

func TestTransferService_IdempotencyConflict(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc := setupServices(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_c_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_c_2",
		Name:           "Receiver",
		InitialBalance: 100,
	})

	// First transfer with key K
	req1 := service.CreateTransferRequest{
		IdempotencyKey: "shared_key_100",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}
	_, err := transferSvc.ExecuteTransfer(ctx, req1)
	if err != nil {
		t.Fatalf("initial transfer failed: %v", err)
	}

	// Second transfer with SAME key K but DIFFERENT amount (200)
	req2 := service.CreateTransferRequest{
		IdempotencyKey: "shared_key_100",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         200,
	}
	_, err = transferSvc.ExecuteTransfer(ctx, req2)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestTransferService_IdempotencyConflict_InvalidPayload(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc := setupServices(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_cp_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_cp_2",
		Name:           "Receiver",
		InitialBalance: 100,
	})

	// 1. First transfer with valid payload and key K
	key := "conflict_invalid_payload_key"
	reqValid := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}
	_, err := transferSvc.ExecuteTransfer(ctx, reqValid)
	if err != nil {
		t.Fatalf("initial transfer failed: %v", err)
	}

	// 2. Reuse key K with negative amount: must return ErrIdempotencyConflict, NOT ErrInvalidAmount
	reqNegAmount := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         -50,
	}
	_, err = transferSvc.ExecuteTransfer(ctx, reqNegAmount)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict on negative amount reuse, got %v", err)
	}

	// 3. Reuse key K with zero amount: must return ErrIdempotencyConflict, NOT ErrInvalidAmount
	reqZeroAmount := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         0,
	}
	_, err = transferSvc.ExecuteTransfer(ctx, reqZeroAmount)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict on zero amount reuse, got %v", err)
	}

	// 4. Reuse key K with missing from_wallet_id: must return ErrIdempotencyConflict, NOT ErrWalletNotFound
	reqMissingWallet := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   "",
		ToWalletID:     w2.ID,
		Amount:         100,
	}
	_, err = transferSvc.ExecuteTransfer(ctx, reqMissingWallet)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict on missing wallet reuse, got %v", err)
	}

	// 5. Reuse key K with same wallet ID: must return ErrIdempotencyConflict, NOT ErrSameWalletTransfer
	reqSameWallet := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w1.ID,
		Amount:         100,
	}
	_, err = transferSvc.ExecuteTransfer(ctx, reqSameWallet)
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict on same-wallet reuse, got %v", err)
	}

	// 6. Contrast with a BRAND NEW key: invalid payloads must return their specific validation errors
	newKey := "brand_new_unused_key"
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: newKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         -10,
	})
	if !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("expected ErrInvalidAmount for brand new key with negative amount, got %v", err)
	}

	// Missing idempotency key
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrMissingIdempotencyKey) {
		t.Fatalf("expected ErrMissingIdempotencyKey for empty idempotency key, got %v", err)
	}

	// Missing wallet ID (empty string)
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: newKey,
		FromWalletID:   "",
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrMissingWalletID) {
		t.Fatalf("expected ErrMissingWalletID for brand new key with missing wallet, got %v", err)
	}

	// Non-existent wallet ID (wallet does not exist in DB)
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "non_existent_wallet_test_key",
		FromWalletID:   "non_existent_sender_id",
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound when wallet does not exist in DB, got %v", err)
	}
}

func TestTransferService_ConcurrentDebits_NoDoubleSpend(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	transferSvc, walletSvc := setupServices(t)

	// Seed source wallet with 1,000 cents ($10.00)
	wFrom, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_hotspot",
		Name:           "Hot Wallet",
		InitialBalance: 1000,
	})
	wTo, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_sink",
		Name:           "Sink Wallet",
		InitialBalance: 0,
	})

	// Launch 10 concurrent requests, each attempting to debit 200 cents ($2.00)
	// Total requested = 10 * 200 = 2,000 cents.
	// Only 5 can possibly succeed (5 * 200 = 1,000). The remaining 5 must fail with insufficient funds.
	const totalRoutines = 10
	var wg sync.WaitGroup
	wg.Add(totalRoutines)

	type result struct {
		success bool
		err     error
	}
	results := make([]result, totalRoutines)

	for i := 0; i < totalRoutines; i++ {
		idx := i
		go func() {
			defer wg.Done()
			req := service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent_debit_key_%d", idx),
				FromWalletID:   wFrom.ID,
				ToWalletID:     wTo.ID,
				Amount:         200,
			}
			_, err := transferSvc.ExecuteTransfer(ctx, req)
			if err == nil {
				results[idx] = result{success: true}
			} else {
				results[idx] = result{success: false, err: err}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Succeeded within timeout
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for concurrent transfers to complete (possible deadlock or blocked row lock)")
	}

	var successCount, failedCount int
	for _, r := range results {
		if r.success {
			successCount++
		} else {
			failedCount++
			if !errors.Is(r.err, domain.ErrInsufficientFunds) {
				t.Errorf("expected ErrInsufficientFunds, got: %v", r.err)
			}
		}
	}

	if successCount != 5 {
		t.Fatalf("expected exactly 5 successes, got %d", successCount)
	}
	if failedCount != 5 {
		t.Fatalf("expected exactly 5 failures, got %d", failedCount)
	}

	// Validate final balances
	finalFrom, _ := walletSvc.GetWallet(ctx, wFrom.ID)
	if finalFrom.Balance != 0 {
		t.Fatalf("expected source wallet balance to be exactly 0, got %d", finalFrom.Balance)
	}

	finalTo, _ := walletSvc.GetWallet(ctx, wTo.ID)
	if finalTo.Balance != 1000 {
		t.Fatalf("expected sink wallet balance to be exactly 1000, got %d", finalTo.Balance)
	}

	// Verify ledger reconciliation for both wallets
	_, _, balancedFrom, err := walletSvc.ReconcileBalance(ctx, wFrom.ID)
	if err != nil || !balancedFrom {
		t.Fatalf("source wallet ledger is not balanced: %v", err)
	}

	_, _, balancedTo, err := walletSvc.ReconcileBalance(ctx, wTo.ID)
	if err != nil || !balancedTo {
		t.Fatalf("sink wallet ledger is not balanced: %v", err)
	}
}

func TestTransferService_ConcurrentBidirectional_NoDeadlocks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	transferSvc, walletSvc := setupServices(t)

	// Seed two wallets with ample balance
	wA, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_bidir_A",
		Name:           "Wallet A",
		InitialBalance: 10000,
	})
	wB, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "wallet_bidir_B",
		Name:           "Wallet B",
		InitialBalance: 10000,
	})

	// Concurrently perform:
	// 20 transfers of 10 from A to B
	// 20 transfers of 10 from B to A
	// If deterministic lock ordering works, NO deadlocks will happen!
	const count = 20
	var wg sync.WaitGroup
	wg.Add(count * 2)

	errorsChan := make(chan error, count*2)

	// Transfers A -> B
	for i := 0; i < count; i++ {
		go func(idx int) {
			defer wg.Done()
			req := service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("bidir_a_to_b_%s", uuid.NewString()),
				FromWalletID:   wA.ID,
				ToWalletID:     wB.ID,
				Amount:         10,
			}
			_, err := transferSvc.ExecuteTransfer(ctx, req)
			if err != nil {
				errorsChan <- fmt.Errorf("A->B error: %w", err)
			}
		}(i)
	}

	// Transfers B -> A
	for i := 0; i < count; i++ {
		go func(idx int) {
			defer wg.Done()
			req := service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("bidir_b_to_a_%s", uuid.NewString()),
				FromWalletID:   wB.ID,
				ToWalletID:     wA.ID,
				Amount:         10,
			}
			_, err := transferSvc.ExecuteTransfer(ctx, req)
			if err != nil {
				errorsChan <- fmt.Errorf("B->A error: %w", err)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Succeeded within timeout
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for concurrent bidirectional transfers to complete (possible deadlock or blocked row lock)")
	}
	close(errorsChan)

	for err := range errorsChan {
		t.Fatalf("detected deadlock or concurrent transfer error: %v", err)
	}

	// Balances should both remain 10,000 because 20*10 left and 20*10 arrived
	finalA, _ := walletSvc.GetWallet(ctx, wA.ID)
	finalB, _ := walletSvc.GetWallet(ctx, wB.ID)

	if finalA.Balance != 10000 {
		t.Fatalf("expected wallet A balance 10000, got %d", finalA.Balance)
	}
	if finalB.Balance != 10000 {
		t.Fatalf("expected wallet B balance 10000, got %d", finalB.Balance)
	}
}

func TestTransferService_IdempotencyInProgress(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_in_progress_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_in_progress_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	req := service.CreateTransferRequest{
		IdempotencyKey: "in_progress_key_1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	// Pre-reserve the key with status IN_PROGRESS to simulate an active concurrent execution
	reqHash := domain.ComputeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    reqHash,
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to seed in-progress idempotency reservation: %v", err)
	}

	// Overlapping concurrent request must immediately receive ErrIdempotencyInProgress without blocking
	_, err = transferSvc.ExecuteTransfer(ctx, req)
	if !errors.Is(err, domain.ErrIdempotencyInProgress) {
		t.Fatalf("expected ErrIdempotencyInProgress, got %v", err)
	}
}

func TestTransferService_StaleOwnerRecovery(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	// Configure a short stale timeout for testing
	transferSvc.SetStaleTimeout(50 * time.Millisecond)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_stale_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_stale_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	req := service.CreateTransferRequest{
		IdempotencyKey: "stale_key_1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	// Seed an IN_PROGRESS reservation
	reqHash := domain.ComputeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount)
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    reqHash,
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to seed in-progress idempotency reservation: %v", err)
	}

	// Wait for the stale timeout to elapse
	time.Sleep(70 * time.Millisecond)

	// The stale lock should be reclaimed and the transfer should succeed
	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("expected successful transfer after stale lock recovery, got %v", err)
	}
	if resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected transfer status PROCESSED, got %s", resp.Status)
	}

	// Verify balance was updated
	w1After, _ := walletSvc.GetWallet(ctx, w1.ID)
	if w1After.Balance != 900 {
		t.Fatalf("expected sender balance 900, got %d", w1After.Balance)
	}
	w2After, _ := walletSvc.GetWallet(ctx, w2.ID)
	if w2After.Balance != 100 {
		t.Fatalf("expected receiver balance 100, got %d", w2After.Balance)
	}
}

func TestTransferService_StaleOwnerRecovery_DifferentPayload_ReturnsConflict(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	// Configure a short stale timeout for testing
	transferSvc.SetStaleTimeout(50 * time.Millisecond)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_stale_diff_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_stale_diff_2",
		Name:           "Receiver 1",
		InitialBalance: 0,
	})
	w3, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_stale_diff_3",
		Name:           "Receiver 2",
		InitialBalance: 0,
	})

	const idempotencyKey = "stale_diff_key_1"

	// Seed an IN_PROGRESS reservation with payload (w1 -> w2, amount 100)
	originalHash := domain.ComputeRequestHash(w1.ID, w2.ID, 100)
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: idempotencyKey,
		RequestHash:    originalHash,
		OwnerToken:     "old_owner_token",
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to seed in-progress idempotency reservation: %v", err)
	}

	// Wait for the stale timeout to elapse
	time.Sleep(70 * time.Millisecond)

	// Attempt executing under the same idempotency key but with a different payload (w1 -> w3, amount 100)
	reqDiff := service.CreateTransferRequest{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w3.ID,
		Amount:         100,
	}

	resp, err := transferSvc.ExecuteTransfer(ctx, reqDiff)
	if err == nil {
		t.Fatalf("expected ErrIdempotencyConflict for different payload on stale key, got response: %+v", resp)
	}
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}

	// Verify the original record was NOT overwritten in the database
	rec, err := repos.Idempotency.GetIdempotency(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("failed to fetch idempotency record: %v", err)
	}
	if rec.RequestHash != originalHash {
		t.Fatalf("expected request_hash to remain %q, but was overwritten with %q", originalHash, rec.RequestHash)
	}

	// Verify balances remain completely unchanged
	w1After, _ := walletSvc.GetWallet(ctx, w1.ID)
	if w1After.Balance != 1000 {
		t.Fatalf("expected sender balance to remain 1000, got %d", w1After.Balance)
	}
	w3After, _ := walletSvc.GetWallet(ctx, w3.ID)
	if w3After.Balance != 0 {
		t.Fatalf("expected receiver 2 balance to remain 0, got %d", w3After.Balance)
	}
}

func TestIdempotencyRepo_StaleReclaim_RequiresMatchingRequestHash(t *testing.T) {
	ctx := context.Background()
	_, _, repos := setupServicesWithRepos(t)

	const key = "repo_stale_reclaim_test"
	const hashA = "hash_payload_a"
	const hashB = "hash_payload_b"
	const staleTimeout = 50 * time.Millisecond

	// 1. Initial reservation with hashA
	recA := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    hashA,
		OwnerToken:     "owner_a",
		Status:         domain.IdempotencyStatusInProgress,
	}
	_, isOwnerA, err := repos.Idempotency.ReserveIdempotency(ctx, recA, 30*time.Second)
	if err != nil || !isOwnerA {
		t.Fatalf("expected initial reservation to succeed, err: %v, isOwner: %v", err, isOwnerA)
	}

	// Wait for stale timeout
	time.Sleep(70 * time.Millisecond)

	// 2. Another caller attempts to reclaim the stale reservation with a different hash (hashB)
	recB := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    hashB,
		OwnerToken:     "owner_b",
		Status:         domain.IdempotencyStatusInProgress,
	}
	returnedB, isOwnerB, err := repos.Idempotency.ReserveIdempotency(ctx, recB, staleTimeout)
	if err != nil {
		t.Fatalf("unexpected error during reservation attempt with hashB: %v", err)
	}
	if isOwnerB {
		t.Fatalf("expected isOwner=false when attempting to reclaim stale reservation with different RequestHash")
	}
	if returnedB.RequestHash != hashA {
		t.Fatalf("expected returned record to have existing hashA, got %s", returnedB.RequestHash)
	}

	// Verify database record still has hashA
	dbRec, err := repos.Idempotency.GetIdempotency(ctx, key)
	if err != nil || dbRec == nil {
		t.Fatalf("failed to query idempotency record: %v", err)
	}
	if dbRec.RequestHash != hashA {
		t.Fatalf("database record was corrupted with %s, expected %s", dbRec.RequestHash, hashA)
	}

	// 3. Caller with matching hashA attempts to reclaim the stale reservation
	recA2 := &domain.IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    hashA,
		OwnerToken:     "owner_a_retry",
		Status:         domain.IdempotencyStatusInProgress,
	}
	returnedA2, isOwnerA2, err := repos.Idempotency.ReserveIdempotency(ctx, recA2, staleTimeout)
	if err != nil {
		t.Fatalf("unexpected error during reclaim with matching hash: %v", err)
	}
	if !isOwnerA2 {
		t.Fatalf("expected isOwner=true when reclaiming stale reservation with matching RequestHash")
	}
	if returnedA2.OwnerToken != "owner_a_retry" {
		t.Fatalf("expected owner token to be updated to owner_a_retry, got %s", returnedA2.OwnerToken)
	}
}

func TestTransferService_DeferredCleanupOnFailure(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_cleanup_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_cleanup_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	req := service.CreateTransferRequest{
		IdempotencyKey: "cleanup_key_1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	// Create a pre-cancelled context to simulate an unexpected abort/timeout during transfer execution
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, err := transferSvc.ExecuteTransfer(cancelledCtx, req)
	if err == nil {
		t.Fatalf("expected transfer with cancelled context to fail")
	}

	// The deferred cleanup should have deleted the IN_PROGRESS reservation.
	// Verify that the key is not locked in idempotency_records:
	rec, err := repos.Idempotency.GetIdempotency(ctx, req.IdempotencyKey)
	if err != nil {
		t.Fatalf("unexpected error fetching idempotency record: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected idempotency record to be deleted by deferred cleanup, but found record with status %s", rec.Status)
	}

	// Subsequent transfer with the same key should succeed immediately without waiting for stale timeout
	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("retry after failure failed: %v", err)
	}
	if resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected transfer status PROCESSED, got %s", resp.Status)
	}
}

func TestTransferService_DestinationBalanceOverflow(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	// Create sender wallet with 500
	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_ovf_sender",
		Name:           "Sender",
		InitialBalance: 500,
	})
	if err != nil {
		t.Fatalf("failed to create sender wallet: %v", err)
	}

	// Create destination wallet with near-maximum balance (math.MaxInt64 - 50)
	destInitialBalance := int64(math.MaxInt64 - 50)
	w2 := &domain.Wallet{
		ID:       "w_ovf_dest",
		Name:     "Destination",
		Balance:  destInitialBalance,
		Currency: "USD",
	}
	if err := repos.Wallets.CreateWallet(ctx, w2); err != nil {
		t.Fatalf("failed to create destination wallet: %v", err)
	}

	// Attempt to transfer 100 to destination wallet, which would exceed math.MaxInt64
	req := service.CreateTransferRequest{
		IdempotencyKey: "key_overflow_test_1",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	if err == nil {
		t.Fatalf("expected ErrBalanceOverflow, but got nil error and resp: %+v", resp)
	}
	if !errors.Is(err, domain.ErrBalanceOverflow) {
		t.Fatalf("expected errors.Is(err, domain.ErrBalanceOverflow), got %v", err)
	}

	// Verify sender balance is unchanged (500)
	w1After, err := walletSvc.GetWallet(ctx, w1.ID)
	if err != nil {
		t.Fatalf("failed to get sender wallet: %v", err)
	}
	if w1After.Balance != 500 {
		t.Fatalf("expected sender balance to remain 500, got %d", w1After.Balance)
	}

	// Verify destination balance is unchanged (math.MaxInt64 - 50)
	w2After, err := walletSvc.GetWallet(ctx, w2.ID)
	if err != nil {
		t.Fatalf("failed to get destination wallet: %v", err)
	}
	if w2After.Balance != destInitialBalance {
		t.Fatalf("expected destination balance to remain %d, got %d", destInitialBalance, w2After.Balance)
	}

	// Verify no idempotency lock is left stuck in IN_PROGRESS
	rec, err := repos.Idempotency.GetIdempotency(ctx, req.IdempotencyKey)
	if err != nil {
		t.Fatalf("failed to get idempotency record: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected idempotency record to be cleaned up after overflow abort, but found: %+v", rec)
	}
}

func TestWalletService_SystemTreasuryReconciliation(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, _ := setupServicesWithRepos(t)

	const treasuryOpeningBalance int64 = 100000000000000

	// 1. Immediately on clean database before any user wallet is created,
	// system_treasury must reconcile with its opening ledger funding record.
	storedBal, ledgerBal, isBalanced, err := walletSvc.ReconcileBalance(ctx, "system_treasury")
	if err != nil {
		t.Fatalf("failed to reconcile system_treasury: %v", err)
	}
	if !isBalanced {
		t.Fatalf("expected system_treasury to be balanced on startup, got stored=%d ledger=%d", storedBal, ledgerBal)
	}
	if storedBal != treasuryOpeningBalance || ledgerBal != treasuryOpeningBalance {
		t.Fatalf("expected balances to equal %d, got stored=%d ledger=%d", treasuryOpeningBalance, storedBal, ledgerBal)
	}

	// 2. Create a user wallet with initial balance 500
	userWallet, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_treasury_test_user",
		Name:           "User",
		InitialBalance: 500,
	})
	if err != nil {
		t.Fatalf("failed to create user wallet: %v", err)
	}

	// 3. User wallet must reconcile
	uStored, uLedger, uBalanced, err := walletSvc.ReconcileBalance(ctx, userWallet.ID)
	if err != nil || !uBalanced {
		t.Fatalf("expected user wallet to be balanced, got stored=%d ledger=%d err=%v", uStored, uLedger, err)
	}
	if uStored != 500 || uLedger != 500 {
		t.Fatalf("expected user wallet balance 500, got stored=%d ledger=%d", uStored, uLedger)
	}

	// 4. System treasury must remain balanced after funding the user wallet
	tStored, tLedger, tBalanced, err := walletSvc.ReconcileBalance(ctx, "system_treasury")
	if err != nil || !tBalanced {
		t.Fatalf("expected system_treasury to remain balanced after user funding, got stored=%d ledger=%d err=%v", tStored, tLedger, err)
	}
	expectedTreasuryBal := treasuryOpeningBalance - 500
	if tStored != expectedTreasuryBal || tLedger != expectedTreasuryBal {
		t.Fatalf("expected treasury balance %d, got stored=%d ledger=%d", expectedTreasuryBal, tStored, tLedger)
	}
}

func TestTransferService_CurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	// Create USD wallet
	wUSD, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_curr_usd",
		Name:           "USD User",
		InitialBalance: 500,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("failed to create USD wallet: %v", err)
	}

	// Create EUR wallet (0 initial balance so no treasury currency conversion is needed)
	wEUR, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_curr_eur",
		Name:           "EUR User",
		InitialBalance: 0,
		Currency:       "EUR",
	})
	if err != nil {
		t.Fatalf("failed to create EUR wallet: %v", err)
	}

	// Attempt transfer from USD to EUR wallet
	req := service.CreateTransferRequest{
		IdempotencyKey: "curr_mismatch_key_1",
		FromWalletID:   wUSD.ID,
		ToWalletID:     wEUR.ID,
		Amount:         100,
	}

	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	if err == nil {
		t.Fatalf("expected ErrCurrencyMismatch, got nil error and resp: %+v", resp)
	}
	if !errors.Is(err, domain.ErrCurrencyMismatch) {
		t.Fatalf("expected errors.Is(err, domain.ErrCurrencyMismatch), got %v", err)
	}

	// Assert balances remained unchanged
	u1, _ := walletSvc.GetWallet(ctx, wUSD.ID)
	if u1.Balance != 500 {
		t.Fatalf("expected USD wallet balance 500, got %d", u1.Balance)
	}
	u2, _ := walletSvc.GetWallet(ctx, wEUR.ID)
	if u2.Balance != 0 {
		t.Fatalf("expected EUR wallet balance 0, got %d", u2.Balance)
	}

	// Assert idempotency record was cleaned up
	rec, err := repos.Idempotency.GetIdempotency(ctx, req.IdempotencyKey)
	if err != nil {
		t.Fatalf("unexpected error querying idempotency: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected idempotency record to be cleaned up after currency mismatch error, but found: %+v", rec)
	}
}

func TestWalletService_CreateWallet_TransactionalRollback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	// Obtain initial treasury balance
	treasuryBefore, err := repos.Wallets.GetWalletByID(ctx, "system_treasury")
	if err != nil {
		t.Fatalf("failed to fetch system treasury: %v", err)
	}

	// Inject a failure AFTER wallet insert by decorating TxManager to fail during ledger persistence.
	// This proves that even after the wallet row is inserted and treasury is debited, a subsequent
	// failure cleanly rolls back the entire transaction.
	failingTxMgr := &failingLedgerTxManager{realTxManager: txManager}
	failingWalletSvc := service.NewWalletService(failingTxMgr, repos)

	failedID := "w_atomic_rollback_" + uuid.NewString()[:8]
	_, err = failingWalletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             failedID,
		Name:           "Rollback User",
		InitialBalance: 500,
		Currency:       "USD",
	})
	if err == nil {
		t.Fatalf("expected error creating wallet with failing ledger persistence, got nil")
	}

	// 1. Verify wallet row was NOT committed (rolled back)
	w, err := repos.Wallets.GetWalletByID(ctx, failedID)
	if err == nil || !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected wallet to not exist after rollback, got %v (wallet: %+v)", err, w)
	}

	// 2. Verify treasury balance was NOT modified (rolled back)
	treasuryAfter, err := repos.Wallets.GetWalletByID(ctx, "system_treasury")
	if err != nil {
		t.Fatalf("failed to fetch system treasury: %v", err)
	}
	if treasuryAfter.Balance != treasuryBefore.Balance {
		t.Fatalf("expected treasury balance to remain %d, got %d", treasuryBefore.Balance, treasuryAfter.Balance)
	}

	// 3. Verify no orphaned ledger entries were created for failedID
	entries, err := repos.Ledger.GetLedgerByWalletID(ctx, failedID, 50, 0)
	if err != nil {
		t.Fatalf("failed to get ledger for failed wallet: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected 0 ledger entries for rolled back wallet, got %d", len(entries))
	}

	// 4. Verify the same ID can now be created cleanly with the standard service
	wCreated, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             failedID,
		Name:           "USD User",
		InitialBalance: 100,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("failed to create wallet with reused ID after previous rollback: %v", err)
	}
	if wCreated.ID != failedID || wCreated.Balance != 100 {
		t.Fatalf("unexpected wallet state: %+v", wCreated)
	}
}

func TestWalletService_ReconcileBalance_ConcurrentTransfers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	transferSvc, walletSvc := setupServices(t)

	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "reconcile_conc_w1",
		Name:           "Conc Sender",
		InitialBalance: 10000,
	})
	if err != nil {
		t.Fatalf("failed to create wallet 1: %v", err)
	}

	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "reconcile_conc_w2",
		Name:           "Conc Receiver",
		InitialBalance: 0,
	})
	if err != nil {
		t.Fatalf("failed to create wallet 2: %v", err)
	}

	var wg sync.WaitGroup
	stopReconcile := make(chan struct{})
	reconcileErrCh := make(chan error, 50)

	// Run concurrent transfers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, _ = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("reconcile-tx-%d", idx),
				FromWalletID:   w1.ID,
				ToWalletID:     w2.ID,
				Amount:         50,
			})
		}(i)
	}

	// Concurrently reconcile balance
	reconcileWg := sync.WaitGroup{}
	reconcileWg.Add(1)
	go func() {
		defer reconcileWg.Done()
		for {
			select {
			case <-stopReconcile:
				return
			default:
				stored, ledger, isBalanced, err := walletSvc.ReconcileBalance(ctx, w1.ID)
				if err != nil {
					reconcileErrCh <- fmt.Errorf("reconcile error: %w", err)
					return
				}
				if !isBalanced {
					reconcileErrCh <- fmt.Errorf("false mismatch detected! stored=%d, ledger=%d", stored, ledger)
					return
				}
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Completed within timeout
	case <-time.After(15 * time.Second):
		close(stopReconcile)
		t.Fatal("timed out waiting for concurrent transfers to complete (possible deadlock or blocked row lock)")
	}

	close(stopReconcile)
	reconcileWg.Wait()
	close(reconcileErrCh)

	for err := range reconcileErrCh {
		t.Fatal(err)
	}

	// Final verification
	stored, ledger, isBalanced, err := walletSvc.ReconcileBalance(ctx, w1.ID)
	if err != nil || !isBalanced {
		t.Fatalf("final reconcile failed: stored=%d, ledger=%d, balanced=%v, err=%v", stored, ledger, isBalanced, err)
	}
}

// TestTransferService_ConcurrentReservationRace_SingleWinner exercises two callers
// simultaneously racing to reserve the same brand-new idempotency key.
// It verifies that exactly one caller wins the reservation and executes the transfer,
// while the losing caller either gets 409 InProgress or replays the completed result,
// guaranteeing zero double-spending and zero duplicate transfers.
func TestTransferService_ConcurrentReservationRace_SingleWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	transferSvc, walletSvc, _ := setupServicesWithRepos(t)

	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_race_src_" + uuid.NewString()[:8],
		Name:           "Sender",
		InitialBalance: 1000,
	})
	if err != nil {
		t.Fatalf("failed to create source wallet: %v", err)
	}

	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_race_dst_" + uuid.NewString()[:8],
		Name:           "Receiver",
		InitialBalance: 0,
	})
	if err != nil {
		t.Fatalf("failed to create destination wallet: %v", err)
	}

	sharedKey := "race_key_" + uuid.NewString()
	req := service.CreateTransferRequest{
		IdempotencyKey: sharedKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         200,
	}

	const callers = 2
	var wg sync.WaitGroup
	wg.Add(callers)

	startBarrier := make(chan struct{})
	type callerResult struct {
		resp *service.TransferResponse
		err  error
	}
	results := make([]callerResult, callers)

	for i := 0; i < callers; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startBarrier
			resp, err := transferSvc.ExecuteTransfer(ctx, req)
			results[idx] = callerResult{resp: resp, err: err}
		}(i)
	}

	// Release both callers simultaneously
	close(startBarrier)

	waitDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for concurrent reservation race callers to finish")
	}

	var newExecutions int
	var inProgressCount int
	var replayCount int

	for _, r := range results {
		if r.err == nil && r.resp != nil {
			if r.resp.IsReplay {
				replayCount++
			} else {
				newExecutions++
			}
		} else if errors.Is(r.err, domain.ErrIdempotencyInProgress) {
			inProgressCount++
		} else {
			t.Fatalf("unexpected result: resp=%v, err=%v", r.resp, r.err)
		}
	}

	// Exactly one caller must execute the new transfer
	if newExecutions != 1 {
		t.Fatalf("expected exactly 1 new execution, got %d (replays=%d, inProgress=%d)", newExecutions, replayCount, inProgressCount)
	}
	// The other caller must either receive ErrIdempotencyInProgress or a replayed response
	if inProgressCount+replayCount != 1 {
		t.Fatalf("expected losing caller to get in_progress or replay, got inProgress=%d, replay=%d", inProgressCount, replayCount)
	}

	// Assert exactly one debit occurred
	updatedW1, err := walletSvc.GetWallet(ctx, w1.ID)
	if err != nil {
		t.Fatalf("failed to get w1: %v", err)
	}
	if updatedW1.Balance != 800 {
		t.Fatalf("expected w1 balance to be 800, got %d", updatedW1.Balance)
	}

	updatedW2, err := walletSvc.GetWallet(ctx, w2.ID)
	if err != nil {
		t.Fatalf("failed to get w2: %v", err)
	}
	if updatedW2.Balance != 200 {
		t.Fatalf("expected w2 balance to be 200, got %d", updatedW2.Balance)
	}

	// Assert reconciliation
	_, _, isBalanced, err := walletSvc.ReconcileBalance(ctx, w1.ID)
	if err != nil || !isBalanced {
		t.Fatalf("w1 reconciliation failed: %v", err)
	}
}

func TestTransferService_IdempotencyKeyLength(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, _ := setupServicesWithRepos(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_len_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_len_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	// Key with 129 characters exceeds the 128 character column limit
	longKey := strings.Repeat("a", 129)
	req := service.CreateTransferRequest{
		IdempotencyKey: longKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	_, err := transferSvc.ExecuteTransfer(ctx, req)
	if !errors.Is(err, domain.ErrInvalidIdempotencyKey) {
		t.Fatalf("expected ErrInvalidIdempotencyKey for 129-char key, got %v", err)
	}
}

func TestLedgerRepository_EnforcesBalancedPair(t *testing.T) {
	ctx := context.Background()
	_, _, repos := setupServicesWithRepos(t)

	// Empty list should return error
	if err := repos.Ledger.CreateLedgerEntries(ctx); !errors.Is(err, domain.ErrInvalidLedgerPair) {
		t.Fatalf("expected ErrInvalidLedgerPair for empty entries, got %v", err)
	}

	// Single entry with transfer ID should return error
	transferID := uuid.NewString()
	singleEntry := domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transferID,
		WalletID:   "w1",
		Type:       domain.LedgerEntryTypeDebit,
		Amount:     100,
	}
	if err := repos.Ledger.CreateLedgerEntries(ctx, singleEntry); !errors.Is(err, domain.ErrInvalidLedgerPair) {
		t.Fatalf("expected ErrInvalidLedgerPair for single transfer entry, got %v", err)
	}

	// Unbalanced amounts should return error
	debit := domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transferID,
		WalletID:   "w1",
		Type:       domain.LedgerEntryTypeDebit,
		Amount:     100,
	}
	creditMismatch := domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transferID,
		WalletID:   "w2",
		Type:       domain.LedgerEntryTypeCredit,
		Amount:     150,
	}
	if err := repos.Ledger.CreateLedgerEntries(ctx, debit, creditMismatch); !errors.Is(err, domain.ErrInvalidLedgerPair) {
		t.Fatalf("expected ErrInvalidLedgerPair for unbalanced amounts, got %v", err)
	}
}

func TestDatabaseTriggers_RejectLedgerMutation_UpdateAndDelete(t *testing.T) {
	ctx := context.Background()
	adminPool := testutil.SetupAdminDB(t)

	repos := repository.Repositories{
		Wallets:     postgres.NewWalletRepository(adminPool),
		Transfers:   postgres.NewTransferRepository(adminPool),
		Ledger:      postgres.NewLedgerRepository(adminPool),
		Idempotency: postgres.NewIdempotencyRepository(adminPool),
	}
	txManager := postgres.NewTxManager(adminPool)
	transferSvc := service.NewTransferService(txManager, repos)
	walletSvc := service.NewWalletService(txManager, repos)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_mut_1",
		Name:           "Mutation Test 1",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_mut_2",
		Name:           "Mutation Test 2",
		InitialBalance: 1000,
	})

	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "key_mut_" + uuid.NewString(),
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         200,
	})
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	var debitID, creditID string
	err = adminPool.QueryRow(ctx, "SELECT id FROM ledger_entries WHERE transfer_id = $1 AND type = 'DEBIT'", resp.TransferID).Scan(&debitID)
	if err != nil {
		t.Fatalf("failed to query debit entry: %v", err)
	}
	err = adminPool.QueryRow(ctx, "SELECT id FROM ledger_entries WHERE transfer_id = $1 AND type = 'CREDIT'", resp.TransferID).Scan(&creditID)
	if err != nil {
		t.Fatalf("failed to query credit entry: %v", err)
	}

	// 1. Direct UPDATE on ledger_entries must be rejected by trg_prevent_ledger_mutation
	_, updateErr := adminPool.Exec(ctx, "UPDATE ledger_entries SET amount = 9999 WHERE id = $1", debitID)
	if updateErr == nil {
		t.Fatalf("expected error updating ledger entry, got nil")
	}
	if !strings.Contains(updateErr.Error(), "ledger entries are immutable: deletions and updates are forbidden") {
		t.Fatalf("expected immutability trigger error, got: %v", updateErr)
	}

	// 2. Direct DELETE on ledger_entries must be rejected by trg_prevent_ledger_mutation
	_, deleteErr := adminPool.Exec(ctx, "DELETE FROM ledger_entries WHERE id = $1", creditID)
	if deleteErr == nil {
		t.Fatalf("expected error deleting ledger entry, got nil")
	}
	if !strings.Contains(deleteErr.Error(), "ledger entries are immutable: deletions and updates are forbidden") {
		t.Fatalf("expected immutability trigger error, got: %v", deleteErr)
	}

	// 3. Direct TRUNCATE on ledger_entries must be rejected by trg_prevent_ledger_truncate
	_, truncateErr := adminPool.Exec(ctx, "TRUNCATE TABLE ledger_entries;")
	if truncateErr == nil {
		t.Fatalf("expected error truncating ledger_entries, got nil")
	}
	if !strings.Contains(truncateErr.Error(), "ledger entries are immutable: deletions and updates are forbidden") {
		t.Fatalf("expected truncate trigger error, got: %v", truncateErr)
	}

	runtimeURL := os.Getenv("TEST_DATABASE_URL")
	if runtimeURL == "" {
		runtimeURL = "postgres://wallet_app:wallet_app_password@localhost:5432/wallet_test_db?sslmode=disable"
	}
	runtimePool, err := postgres.NewPool(ctx, runtimeURL)
	if err != nil {
		t.Fatalf("failed to connect runtime pool: %v", err)
	}
	defer runtimePool.Close()

	_, runtimeUpdateErr := runtimePool.Exec(ctx, "UPDATE ledger_entries SET amount = 9999 WHERE id = $1", debitID)
	if runtimeUpdateErr == nil {
		t.Fatalf("expected runtime role to fail update on ledger_entries, got nil")
	}
	_, runtimeTruncateErr := runtimePool.Exec(ctx, "TRUNCATE TABLE ledger_entries;")
	if runtimeTruncateErr == nil {
		t.Fatalf("expected runtime role to fail truncate on ledger_entries, got nil")
	}

	// 5. Verify ledger entries are still untouched
	var count int
	err = adminPool.QueryRow(ctx, "SELECT count(*) FROM ledger_entries WHERE transfer_id = $1", resp.TransferID).Scan(&count)
	if err != nil || count != 2 {
		t.Fatalf("expected 2 ledger entries, got count=%d, err=%v", count, err)
	}
}

func TestDatabaseConstraints_RejectDuplicateLedgerEntryType(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, _ := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_dup_type_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_dup_type_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transferID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, 100, 'PENDING')
	`, transferID, "dup_type_"+uuid.NewString(), w1.ID, w2.ID)
	if err != nil {
		t.Fatalf("failed to insert transfer: %v", err)
	}

	// Attempt to insert two DEBIT entries for the same transfer: caught by unique index
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
		VALUES ($1, $2, $3, 'DEBIT', 100),
		       ($4, $2, $5, 'DEBIT', 100)
	`, uuid.NewString(), transferID, w1.ID, uuid.NewString(), w2.ID)
	if err == nil {
		t.Fatalf("expected unique constraint error for duplicate DEBIT entries, got nil")
	}
	if !strings.Contains(err.Error(), "idx_ledger_entries_transfer_type") {
		t.Fatalf("expected error referencing idx_ledger_entries_transfer_type, got: %v", err)
	}
}

func TestDatabaseTriggers_MalformedLedgerPairFailsAtCommit(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, _ := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_malformed_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_malformed_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})
	w3, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_malformed_3",
		Name:           "Third Party",
		InitialBalance: 0,
	})

	testCases := []struct {
		name         string
		setupEntries func(tx pgx.Tx, transferID string) error
	}{
		{
			name: "Single debit entry without matching credit",
			setupEntries: func(tx pgx.Tx, transferID string) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
					VALUES ($1, $2, $3, 'DEBIT', 100)
				`, uuid.NewString(), transferID, w1.ID)
				return err
			},
		},
		{
			name: "Single credit entry without matching debit",
			setupEntries: func(tx pgx.Tx, transferID string) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
					VALUES ($1, $2, $3, 'CREDIT', 100)
				`, uuid.NewString(), transferID, w2.ID)
				return err
			},
		},
		{
			name: "Mismatched destination wallet on credit entry",
			setupEntries: func(tx pgx.Tx, transferID string) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
					VALUES ($1, $2, $3, 'DEBIT', 100),
					       ($4, $2, $5, 'CREDIT', 100)
				`, uuid.NewString(), transferID, w1.ID, uuid.NewString(), w3.ID)
				return err
			},
		},
		{
			name: "Mismatched source wallet on debit entry",
			setupEntries: func(tx pgx.Tx, transferID string) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
					VALUES ($1, $2, $3, 'DEBIT', 100),
					       ($4, $2, $5, 'CREDIT', 100)
				`, uuid.NewString(), transferID, w3.ID, uuid.NewString(), w2.ID)
				return err
			},
		},
		{
			name: "Credit entry amount differs from transfer amount",
			setupEntries: func(tx pgx.Tx, transferID string) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
					VALUES ($1, $2, $3, 'DEBIT', 100),
					       ($4, $2, $5, 'CREDIT', 50)
				`, uuid.NewString(), transferID, w1.ID, uuid.NewString(), w2.ID)
				return err
			},
		},
		{
			name: "Debit entry amount differs from transfer amount",
			setupEntries: func(tx pgx.Tx, transferID string) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount)
					VALUES ($1, $2, $3, 'DEBIT', 75),
					       ($4, $2, $5, 'CREDIT', 100)
				`, uuid.NewString(), transferID, w1.ID, uuid.NewString(), w2.ID)
				return err
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatalf("failed to begin tx: %v", err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			transferID := uuid.NewString()
			_, err = tx.Exec(ctx, `
				INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
				VALUES ($1, $2, $3, $4, 100, 'PROCESSED')
			`, transferID, "idem_"+uuid.NewString(), w1.ID, w2.ID)
			if err != nil {
				t.Fatalf("failed to insert transfer: %v", err)
			}

			if err := tc.setupEntries(tx, transferID); err != nil {
				t.Fatalf("failed to insert test entries: %v", err)
			}

			// Deferred constraint trigger trg_check_ledger_pair fires at commit time
			commitErr := tx.Commit(ctx)
			if commitErr == nil {
				t.Fatalf("expected commit to fail due to deferred constraint trigger, got nil")
			}
			if !strings.Contains(commitErr.Error(), "invalid ledger pair for transfer") {
				t.Fatalf("expected trigger error containing 'invalid ledger pair for transfer', got: %v", commitErr)
			}
		})
	}
}

func TestDatabaseTriggers_ProcessedTransferWithoutLedgerPairFailsAtCommit(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, _ := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_nopair_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_nopair_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Directly insert transfer with status PROCESSED without inserting any ledger entries
	transferID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, 100, 'PROCESSED')
	`, transferID, "nopair_key", w1.ID, w2.ID)
	if err != nil {
		t.Fatalf("failed to insert transfer: %v", err)
	}

	// Commit must be rejected by trg_check_transfer_processed deferred constraint trigger
	commitErr := tx.Commit(ctx)
	if commitErr == nil {
		t.Fatalf("expected commit to fail for PROCESSED transfer without ledger pair, got nil")
	}
	if !strings.Contains(commitErr.Error(), "cannot commit PROCESSED transfer") {
		t.Fatalf("expected trigger error containing 'cannot commit PROCESSED transfer', got: %v", commitErr)
	}
}

func TestLedgerRepository_DeterministicOrdering_TieBreaker(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	wSource, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_order_src",
		Name:           "Ordering Source Wallet",
		InitialBalance: 1000,
	})
	if err != nil {
		t.Fatalf("failed to create source wallet: %v", err)
	}

	w, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_order_test",
		Name:           "Ordering Test Wallet",
		InitialBalance: 0,
	})
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}

	fixedTime := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	err = txManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		txA := &domain.Transfer{ID: "tx_a", IdempotencyKey: "idem_a_" + uuid.NewString(), FromWalletID: wSource.ID, ToWalletID: w.ID, Amount: 20, Status: domain.TransferStatusProcessed}
		if err := txRepos.Transfers.CreateTransfer(ctx, txA); err != nil {
			return err
		}
		txB := &domain.Transfer{ID: "tx_b", IdempotencyKey: "idem_b_" + uuid.NewString(), FromWalletID: wSource.ID, ToWalletID: w.ID, Amount: 30, Status: domain.TransferStatusProcessed}
		if err := txRepos.Transfers.CreateTransfer(ctx, txB); err != nil {
			return err
		}
		txC := &domain.Transfer{ID: "tx_c", IdempotencyKey: "idem_c_" + uuid.NewString(), FromWalletID: wSource.ID, ToWalletID: w.ID, Amount: 10, Status: domain.TransferStatusProcessed}
		if err := txRepos.Transfers.CreateTransfer(ctx, txC); err != nil {
			return err
		}

		pairC := []domain.LedgerEntry{
			{ID: "c_debit", TransferID: txC.ID, WalletID: wSource.ID, Type: domain.LedgerEntryTypeDebit, Amount: 10, CreatedAt: fixedTime},
			{ID: "c_entry", TransferID: txC.ID, WalletID: w.ID, Type: domain.LedgerEntryTypeCredit, Amount: 10, CreatedAt: fixedTime},
		}
		pairA := []domain.LedgerEntry{
			{ID: "a_debit", TransferID: txA.ID, WalletID: wSource.ID, Type: domain.LedgerEntryTypeDebit, Amount: 20, CreatedAt: fixedTime},
			{ID: "a_entry", TransferID: txA.ID, WalletID: w.ID, Type: domain.LedgerEntryTypeCredit, Amount: 20, CreatedAt: fixedTime},
		}
		pairB := []domain.LedgerEntry{
			{ID: "b_debit", TransferID: txB.ID, WalletID: wSource.ID, Type: domain.LedgerEntryTypeDebit, Amount: 30, CreatedAt: fixedTime},
			{ID: "b_entry", TransferID: txB.ID, WalletID: w.ID, Type: domain.LedgerEntryTypeCredit, Amount: 30, CreatedAt: fixedTime},
		}

		if err := txRepos.Ledger.CreateLedgerEntries(ctx, pairC...); err != nil {
			return err
		}
		if err := txRepos.Ledger.CreateLedgerEntries(ctx, pairA...); err != nil {
			return err
		}
		if err := txRepos.Ledger.CreateLedgerEntries(ctx, pairB...); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to insert test data in transaction: %v", err)
	}

	entries, err := repos.Ledger.GetLedgerByWalletID(ctx, w.ID, 10, 0)
	if err != nil {
		t.Fatalf("failed to get ledger: %v", err)
	}

	// Expect deterministic ascending order by ID: a_entry, b_entry, c_entry
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
	if entries[0].ID != "a_entry" || entries[1].ID != "b_entry" || entries[2].ID != "c_entry" {
		t.Fatalf("expected deterministic ID tie-breaking [a_entry, b_entry, c_entry], got [%s, %s, %s]",
			entries[0].ID, entries[1].ID, entries[2].ID)
	}

	// Verify pagination: limit=1, offset=1 should return exactly b_entry
	page, err := repos.Ledger.GetLedgerByWalletID(ctx, w.ID, 1, 1)
	if err != nil {
		t.Fatalf("failed to get paginated ledger page: %v", err)
	}
	if len(page) != 1 || page[0].ID != "b_entry" {
		t.Fatalf("expected page with exactly [b_entry], got %+v", page)
	}
}

func TestTransferService_PreflightRecheck_ConcurrentIdempotency(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, repos := setupServicesWithRepos(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_prerecheck_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_prerecheck_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	// Simulate a concurrent request that already reserved the key
	key := "concurrent_preflight_key"
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: key,
		RequestHash:    domain.ComputeRequestHash(w1.ID, w2.ID, 100),
		OwnerToken:     "other_owner",
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to seed concurrent reservation: %v", err)
	}

	// Attempt a transfer with the same key but INVALID amount (-50)
	// Must return ErrIdempotencyConflict, NOT ErrInvalidAmount
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         -50,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict for concurrent key with invalid amount, got %v", err)
	}

	// Attempt a transfer with the same key and same parameters (which would otherwise succeed)
	// Must return ErrIdempotencyInProgress (409), not proceed with execution
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrIdempotencyInProgress) {
		t.Fatalf("expected ErrIdempotencyInProgress for active concurrent reservation, got %v", err)
	}
}

type failingCommitTxManager struct {
	realTxManager repository.TxManager
}

func (m *failingCommitTxManager) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	_ = m.realTxManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		return fn(txRepos)
	})
	// Simulate commit returning an error after operations completed
	return errors.New("simulated ambiguous commit failure: commit ACK dropped")
}

func TestTransferService_CommitAmbiguity_PreservesReservation(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_ambig_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_ambig_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	failingTxManager := &failingCommitTxManager{realTxManager: txManager}
	transferSvc := service.NewTransferService(failingTxManager, repos)

	key := "ambiguous_commit_key"
	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	// Because the transaction actually committed in the real DB before the simulated ACK failure,
	// ExecuteTransfer must reconcile the committed outcome and return the successful response
	if err != nil {
		t.Fatalf("expected successful reconciliation of committed transfer, got error: %v", err)
	}
	if resp == nil || resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected PROCESSED response from reconciliation, got %+v", resp)
	}

	// The reservation must NOT be deleted blindly by deferred cleanup; it must be COMPLETED
	rec, getErr := repos.Idempotency.GetIdempotency(ctx, key)
	if getErr != nil {
		t.Fatalf("failed to query idempotency: %v", getErr)
	}
	if rec == nil {
		t.Fatalf("reservation was unexpectedly deleted on ambiguous commit")
	}
	if rec.Status != domain.IdempotencyStatusCompleted {
		t.Fatalf("expected reconciled status COMPLETED, got %s", rec.Status)
	}
}

type conditionalFailingIdempotencyRepo struct {
	repository.IdempotencyRepository
	skipTxUpdate        bool
	failReconcileUpdate bool
}

func (c *conditionalFailingIdempotencyRepo) UpdateIdempotency(ctx context.Context, record *domain.IdempotencyRecord) error {
	if c.skipTxUpdate {
		// Simulate worker crash / drop right before idempotency update inside transaction,
		// leaving the record IN_PROGRESS in the database while transfer commits.
		return nil
	}
	if c.failReconcileUpdate {
		return errors.New("simulated network failure during idempotency finalization")
	}
	return c.IdempotencyRepository.UpdateIdempotency(ctx, record)
}

func TestTransferService_CommitAmbiguity_UpdateIdempotencyFailure_PropagatesErrorAndEnablesRecovery(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_fail_fin_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_fail_fin_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	condRepo := &conditionalFailingIdempotencyRepo{
		IdempotencyRepository: repos.Idempotency,
		failReconcileUpdate:   true,
	}
	repos.Idempotency = condRepo

	failingTx := &failingCommitTxManagerWithHook{
		realTxManager: txManager,
		interceptRepos: func(txRepos repository.Repositories) repository.Repositories {
			// Intercept txRepos.Idempotency so it does not update DB inside the tx,
			// leaving the DB row in IN_PROGRESS when ambiguous commit returns.
			txRepos.Idempotency = &conditionalFailingIdempotencyRepo{
				IdempotencyRepository: txRepos.Idempotency,
				skipTxUpdate:          true,
			}
			return txRepos
		},
	}

	transferSvc := service.NewTransferService(failingTx, repos)

	key := "ambiguous_commit_failing_finalization_key"
	req := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	resp, err := transferSvc.ExecuteTransfer(ctx, req)
	// Because finalization UpdateIdempotency failed, the service must NOT return success:
	// it must propagate the update error!
	if err == nil {
		t.Fatalf("expected error from failed idempotency finalization, got response: %+v", resp)
	}
	if !strings.Contains(err.Error(), "failed to finalize idempotency record") &&
		!strings.Contains(err.Error(), "simulated network failure during idempotency finalization") {
		t.Fatalf("expected finalization error message, got: %v", err)
	}

	// The reservation must NOT be deleted by deferred cleanup; it must remain IN_PROGRESS for recovery
	rec, getErr := repos.Idempotency.GetIdempotency(ctx, key)
	if getErr != nil {
		t.Fatalf("failed to query idempotency: %v", getErr)
	}
	if rec == nil {
		t.Fatalf("reservation was unexpectedly deleted on failed finalization")
	}
	if rec.Status != domain.IdempotencyStatusInProgress {
		t.Fatalf("expected status IN_PROGRESS, got %s", rec.Status)
	}

	// Now network recovers: disable failReconcileUpdate
	condRepo.failReconcileUpdate = false

	// Create normal transfer service and set short stale timeout so recovery can proceed
	normalTransferSvc := service.NewTransferService(txManager, repos)
	normalTransferSvc.SetStaleTimeout(50 * time.Millisecond)

	time.Sleep(70 * time.Millisecond)

	// Caller retries the transfer with the same request
	retryResp, retryErr := normalTransferSvc.ExecuteTransfer(ctx, req)
	if retryErr != nil {
		t.Fatalf("expected successful recovery on retry, got: %v", retryErr)
	}
	if retryResp == nil || retryResp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected PROCESSED response on retry, got %+v", retryResp)
	}
	if !retryResp.IsReplay {
		t.Fatalf("expected IsReplay=true on recovered retry")
	}

	// Verify wallet balances were debited only ONCE (no duplicate transfer!)
	w1After, _ := walletSvc.GetWallet(ctx, w1.ID)
	if w1After.Balance != 900 {
		t.Fatalf("expected sender balance 900, got %d", w1After.Balance)
	}
	w2After, _ := walletSvc.GetWallet(ctx, w2.ID)
	if w2After.Balance != 100 {
		t.Fatalf("expected receiver balance 100, got %d", w2After.Balance)
	}
}

type operationalFailingTransferRepo struct {
	repository.TransferRepository
	failGetByID bool
}

func (r *operationalFailingTransferRepo) GetTransferByID(ctx context.Context, id string) (*domain.Transfer, error) {
	if r.failGetByID {
		return nil, errors.New("simulated operational database timeout during transfer lookup")
	}
	return r.TransferRepository.GetTransferByID(ctx, id)
}

func TestTransferService_CommitAmbiguity_GetTransferOperationalError_RetainsReservation(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_operr_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_operr_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	transRepo := &operationalFailingTransferRepo{
		TransferRepository: repos.Transfers,
		failGetByID:        false,
	}
	repos.Transfers = transRepo

	failingTx := &failingCommitTxManagerWithHook{
		realTxManager: txManager,
		interceptRepos: func(txRepos repository.Repositories) repository.Repositories {
			// Leave idempotency record IN_PROGRESS in DB during tx
			txRepos.Idempotency = &conditionalFailingIdempotencyRepo{
				IdempotencyRepository: txRepos.Idempotency,
				skipTxUpdate:          true,
			}
			return txRepos
		},
		afterTx: func() {
			// Simulate operational error during reconciliation GetTransferByID
			transRepo.failGetByID = true
		},
	}

	transferSvc := service.NewTransferService(failingTx, repos)

	key := "ambiguous_commit_operr_key"
	req := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	_, err := transferSvc.ExecuteTransfer(ctx, req)
	if err == nil {
		t.Fatalf("expected error from ambiguous commit execution, got nil")
	}

	// An operational error (database outage/timeout) during reconciliation must NOT treat the transfer
	// as rolled back. The reservation must be retained as IN_PROGRESS, NOT deleted!
	rec, getErr := repos.Idempotency.GetIdempotency(ctx, key)
	if getErr != nil {
		t.Fatalf("failed to query idempotency: %v", getErr)
	}
	if rec == nil {
		t.Fatalf("reservation was unexpectedly deleted on operational error during reconciliation!")
	}
	if rec.Status != domain.IdempotencyStatusInProgress {
		t.Fatalf("expected status to remain IN_PROGRESS, got %s", rec.Status)
	}
}

func TestTransferService_CommitAmbiguity_ConfirmedRollback_DeletesReservation(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_rollback_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_rollback_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	// Rollback TxManager: simulates complete rollback of the transaction
	rollbackTx := &rollbackCommitTxManager{realTxManager: txManager}
	transferSvc := service.NewTransferService(rollbackTx, repos)

	key := "ambiguous_commit_confirmed_rollback_key"
	req := service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	_, err := transferSvc.ExecuteTransfer(ctx, req)
	if err == nil {
		t.Fatalf("expected error from rolled back transfer, got nil")
	}

	// When confirmed rolled back (GetTransferByID returns ErrTransferNotFound),
	// the in-progress reservation SHOULD be deleted so retries can proceed cleanly.
	rec, getErr := repos.Idempotency.GetIdempotency(ctx, key)
	if getErr != nil {
		t.Fatalf("failed to query idempotency: %v", getErr)
	}
	if rec != nil {
		t.Fatalf("expected reservation to be deleted on confirmed rollback, but found: %+v", rec)
	}
}

type rollbackCommitTxManager struct {
	realTxManager repository.TxManager
}

func (m *rollbackCommitTxManager) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	_ = m.realTxManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		_ = fn(txRepos)
		return errors.New("force transaction rollback")
	})
	return errors.New("simulated commit failure with rollback")
}

type failingCommitTxManagerWithHook struct {
	realTxManager  repository.TxManager
	afterTx        func()
	interceptRepos func(txRepos repository.Repositories) repository.Repositories
}

func (m *failingCommitTxManagerWithHook) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	_ = m.realTxManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		wrapped := txRepos
		if m.interceptRepos != nil {
			wrapped = m.interceptRepos(txRepos)
		}
		return fn(wrapped)
	})
	if m.afterTx != nil {
		m.afterTx()
	}
	return errors.New("simulated ambiguous commit failure: commit ACK dropped")
}

type failingHeartbeatIdempotencyRepo struct {
	repository.IdempotencyRepository
}

func (f *failingHeartbeatIdempotencyRepo) HeartbeatIdempotency(ctx context.Context, key string, ownerToken string) error {
	return errors.New("simulated network failure during heartbeat lease refresh")
}

type delayTxManager struct {
	realTxManager repository.TxManager
	delay         time.Duration
}

func (m *delayTxManager) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	return m.realTxManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		time.Sleep(m.delay)
		return fn(txRepos)
	})
}

func TestTransferService_HeartbeatFailure_AbortsTransaction(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, txManager, repos := setupServicesWithTxManager(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_hbfail_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_hbfail_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	repos.Idempotency = &failingHeartbeatIdempotencyRepo{IdempotencyRepository: repos.Idempotency}
	slowTxManager := &delayTxManager{realTxManager: txManager, delay: 50 * time.Millisecond}

	transferSvc := service.NewTransferService(slowTxManager, repos)
	transferSvc.SetHeartbeatInterval(10 * time.Millisecond)
	transferSvc.SetStaleTimeout(40 * time.Millisecond)

	key := "heartbeat_failure_abort_key"
	_, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if err == nil {
		t.Fatalf("expected error from unrefreshable heartbeat failure, got nil")
	}
	if !strings.Contains(err.Error(), "heartbeat lease refresh failed reliably") {
		t.Fatalf("expected error containing 'heartbeat lease refresh failed reliably', got: %v", err)
	}
}

func TestTransferService_SystemTreasuryRestricted(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc := setupServices(t)

	w, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_treasury_restricted_user",
		Name:           "Normal User",
		InitialBalance: 1000,
	})
	if err != nil {
		t.Fatalf("failed to create user wallet: %v", err)
	}

	// 1. FromWalletID == system_treasury must be rejected
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "treasury_restricted_from_key",
		FromWalletID:   domain.SystemTreasuryWalletID,
		ToWalletID:     w.ID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrSystemTreasuryRestricted) {
		t.Fatalf("expected ErrSystemTreasuryRestricted when transferring from system_treasury, got: %v", err)
	}

	// 2. ToWalletID == system_treasury must be rejected
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "treasury_restricted_to_key",
		FromWalletID:   w.ID,
		ToWalletID:     domain.SystemTreasuryWalletID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrSystemTreasuryRestricted) {
		t.Fatalf("expected ErrSystemTreasuryRestricted when transferring to system_treasury, got: %v", err)
	}
}

func TestWalletService_CreateWallet_PublicContextRestricted(t *testing.T) {
	ctx := context.Background()
	_, walletSvc := setupServices(t)

	// Context marked with public caller attempting InitialBalance > 0
	publicCtx := service.ContextWithPublicCaller(ctx)
	_, err := walletSvc.CreateWallet(publicCtx, service.CreateWalletRequest{
		ID:             "public_unauth_funding_test",
		Name:           "Public Hacker",
		InitialBalance: 500,
	})
	if !errors.Is(err, domain.ErrUnauthorizedFunding) {
		t.Fatalf("expected ErrUnauthorizedFunding for public context with positive initial balance, got: %v", err)
	}

	// Context marked with public caller creating wallet with InitialBalance == 0 must succeed
	w, err := walletSvc.CreateWallet(publicCtx, service.CreateWalletRequest{
		ID:             "public_zero_bal_test",
		Name:           "Public Good Citizen",
		InitialBalance: 0,
	})
	if err != nil {
		t.Fatalf("expected success for public context with zero initial balance, got: %v", err)
	}
	if w.Balance != 0 {
		t.Fatalf("expected balance 0, got %d", w.Balance)
	}
}

func TestWalletService_CreateWallet_SystemTreasuryIDForbidden(t *testing.T) {
	ctx := context.Background()
	_, walletSvc := setupServices(t)

	// Attempting to create a wallet with ID == system_treasury must fail
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             domain.SystemTreasuryWalletID,
		Name:           "Fake Treasury",
		InitialBalance: 0,
	})
	if !errors.Is(err, domain.ErrWalletAlreadyExists) {
		t.Fatalf("expected ErrWalletAlreadyExists when creating wallet with system_treasury ID, got: %v", err)
	}
}

func TestTransferRepository_PendingToTerminalStatusEnforcement(t *testing.T) {
	ctx := context.Background()
	_, _, pool, repos := setupServicesWithPool(t)
	txManager := postgres.NewTxManager(pool)

	w1ID := "w_status_1_" + uuid.NewString()
	w2ID := "w_status_2_" + uuid.NewString()
	if err := repos.Wallets.CreateWallet(ctx, &domain.Wallet{
		ID:       w1ID,
		Name:     "Sender",
		Balance:  1000,
		Currency: "USD",
	}); err != nil {
		t.Fatalf("failed to create wallet 1: %v", err)
	}
	if err := repos.Wallets.CreateWallet(ctx, &domain.Wallet{
		ID:       w2ID,
		Name:     "Receiver",
		Balance:  1000,
		Currency: "USD",
	}); err != nil {
		t.Fatalf("failed to create wallet 2: %v", err)
	}

	// 1. Create a transfer and transition PENDING -> PROCESSED atomically with ledger entries
	transferID := uuid.NewString()
	transfer := &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: "test_status_enforce_" + uuid.NewString(),
		FromWalletID:   w1ID,
		ToWalletID:     w2ID,
		Amount:         100,
		Status:         domain.TransferStatusPending,
	}
	if err := repos.Transfers.CreateTransfer(ctx, transfer); err != nil {
		t.Fatalf("failed to create transfer: %v", err)
	}

	now := time.Now().UTC()
	err := txManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		if err := txRepos.Transfers.UpdateTransferStatus(ctx, transferID, domain.TransferStatusProcessed, nil); err != nil {
			return err
		}
		return txRepos.Ledger.CreateLedgerEntries(ctx,
			domain.LedgerEntry{ID: uuid.NewString(), TransferID: transferID, WalletID: w1ID, Type: domain.LedgerEntryTypeDebit, Amount: 100, CreatedAt: now},
			domain.LedgerEntry{ID: uuid.NewString(), TransferID: transferID, WalletID: w2ID, Type: domain.LedgerEntryTypeCredit, Amount: 100, CreatedAt: now},
		)
	})
	if err != nil {
		t.Fatalf("expected atomic PENDING -> PROCESSED with ledger entries to succeed, got %v", err)
	}

	// 2. Attempting to transition already-terminal PROCESSED transfer to FAILED must fail with ErrInvalidStateTransition
	failReason := "duplicate attempt"
	err = repos.Transfers.UpdateTransferStatus(ctx, transferID, domain.TransferStatusFailed, &failReason)
	if !errors.Is(err, domain.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition when transitioning from terminal PROCESSED state, got %v", err)
	}

	// 3. Create another transfer and transition PENDING -> FAILED
	transfer2ID := uuid.NewString()
	transfer2 := &domain.Transfer{
		ID:             transfer2ID,
		IdempotencyKey: "test_status_enforce_2_" + uuid.NewString(),
		FromWalletID:   w1ID,
		ToWalletID:     w2ID,
		Amount:         50,
		Status:         domain.TransferStatusPending,
	}
	if err := repos.Transfers.CreateTransfer(ctx, transfer2); err != nil {
		t.Fatalf("failed to create transfer 2: %v", err)
	}
	failedReason := "insufficient funds"
	if err := repos.Transfers.UpdateTransferStatus(ctx, transfer2ID, domain.TransferStatusFailed, &failedReason); err != nil {
		t.Fatalf("expected transition PENDING -> FAILED to succeed, got %v", err)
	}

	// Attempting to transition already-terminal FAILED transfer must fail with ErrInvalidStateTransition
	err = repos.Transfers.UpdateTransferStatus(ctx, transfer2ID, domain.TransferStatusProcessed, nil)
	if !errors.Is(err, domain.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition when transitioning from terminal FAILED state, got %v", err)
	}

	// 4. Attempting to transition non-existent transfer must fail with ErrTransferNotFound
	err = repos.Transfers.UpdateTransferStatus(ctx, "non_existent_id", domain.TransferStatusProcessed, nil)
	if !errors.Is(err, domain.ErrTransferNotFound) {
		t.Fatalf("expected ErrTransferNotFound for non-existent transfer, got %v", err)
	}
}

func TestDatabaseTriggers_RejectLedgerEntriesForFailedTransfer(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, _ := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_failed_trg_1",
		Name:           "Sender",
		InitialBalance: 1000,
	})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_failed_trg_2",
		Name:           "Receiver",
		InitialBalance: 0,
	})

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transferID := uuid.NewString()
	_, err = tx.Exec(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, 100, 'FAILED')
	`, transferID, "idem_failed_"+uuid.NewString(), w1.ID, w2.ID)
	if err != nil {
		t.Fatalf("failed to insert failed transfer: %v", err)
	}

	// Insert ledger entries for a FAILED transfer
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount, created_at)
		VALUES ($1, $2, $3, 'DEBIT', 100, $5), ($4, $2, $6, 'CREDIT', 100, $5)
	`, uuid.NewString(), transferID, w1.ID, uuid.NewString(), now, w2.ID)
	if err != nil {
		t.Fatalf("failed to insert ledger entries: %v", err)
	}

	// Committing must be rejected by database triggers
	commitErr := tx.Commit(ctx)
	if commitErr == nil {
		t.Fatalf("expected commit to fail when committing ledger entries for a FAILED transfer, got nil")
	}
	if !strings.Contains(commitErr.Error(), "cannot commit FAILED transfer") &&
		!strings.Contains(commitErr.Error(), "transfer status must be PROCESSED at commit time") {
		t.Fatalf("expected trigger error rejecting ledger entries for failed transfer, got: %v", commitErr)
	}
}

func TestWalletService_NilTxManagerFails(t *testing.T) {
	ctx := context.Background()
	_, _, repos := setupServicesWithRepos(t)

	// Construct service with nil txManager
	nilTxSvc := service.NewWalletService(nil, repos)

	// CreateWallet must fail fast
	_, err := nilTxSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_nil_tx",
		Name:           "No Tx",
		InitialBalance: 100,
	})
	if err == nil || !strings.Contains(err.Error(), "transaction manager is required") {
		t.Fatalf("expected error containing 'transaction manager is required', got %v", err)
	}

	// ReconcileBalance must also fail fast
	_, _, _, err = nilTxSvc.ReconcileBalance(ctx, "w_nil_tx")
	if err == nil || !strings.Contains(err.Error(), "transaction manager is required") {
		t.Fatalf("expected error containing 'transaction manager is required', got %v", err)
	}
}

func TestTransferService_StaleRecovery_PendingTransfer_ReturnsInProgress(t *testing.T) {
	ctx := context.Background()
	transferSvc, walletSvc, pool, repos := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_stale_pend_1", Name: "W1", InitialBalance: 500})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_stale_pend_2", Name: "W2", InitialBalance: 500})

	idemKey := "key_stale_pending_" + uuid.NewString()
	requestHash := domain.ComputeRequestHash(w1.ID, w2.ID, 100)

	// 1. Seed stale in-progress reservation (updated 2 minutes ago)
	staleTime := time.Now().UTC().Add(-2 * time.Minute)
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		RequestHash:    requestHash,
		OwnerToken:     uuid.NewString(),
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to reserve idempotency: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE idempotency_records SET updated_at = $1 WHERE idempotency_key = $2", staleTime, idemKey); err != nil {
		t.Fatalf("failed to update idempotency timestamp: %v", err)
	}

	// 2. Insert an underlying transfer with PENDING status
	transferID := uuid.NewString()
	if err := repos.Transfers.CreateTransfer(ctx, &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: idemKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
		Status:         domain.TransferStatusPending,
		CreatedAt:      staleTime,
		UpdatedAt:      staleTime,
	}); err != nil {
		t.Fatalf("failed to create pending transfer: %v", err)
	}

	// 3. Executing transfer must NOT treat PENDING as COMPLETED or return HTTP 201; it must return ErrIdempotencyInProgress
	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: idemKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if !errors.Is(err, domain.ErrIdempotencyInProgress) {
		t.Fatalf("expected ErrIdempotencyInProgress for stale reservation pointing to PENDING transfer, got resp: %+v, err: %v", resp, err)
	}

	// 4. Verify idempotency record remains IN_PROGRESS and was not finalized to COMPLETED
	finalRec, _ := repos.Idempotency.GetIdempotency(ctx, idemKey)
	if finalRec != nil && finalRec.Status == domain.IdempotencyStatusCompleted {
		t.Fatalf("idempotency record was incorrectly finalized to COMPLETED for non-terminal PENDING transfer")
	}
}

type lookupFailingTransferRepo struct {
	repository.TransferRepository
}

func (f *lookupFailingTransferRepo) GetTransferByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	return nil, errors.New("simulated operational DB timeout during transfer lookup")
}

func TestTransferService_StaleRecovery_LookupError_PropagatesError(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, repos := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_stale_err_1", Name: "W1", InitialBalance: 500})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_stale_err_2", Name: "W2", InitialBalance: 500})

	idemKey := "key_stale_err_" + uuid.NewString()
	requestHash := domain.ComputeRequestHash(w1.ID, w2.ID, 100)

	// Seed stale in-progress reservation
	staleTime := time.Now().UTC().Add(-2 * time.Minute)
	_, _, _ = repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		RequestHash:    requestHash,
		OwnerToken:     uuid.NewString(),
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	_, _ = pool.Exec(ctx, "UPDATE idempotency_records SET updated_at = $1 WHERE idempotency_key = $2", staleTime, idemKey)

	// Wrap transfer repo with simulated lookup failure
	faultyRepos := repos
	faultyRepos.Transfers = &lookupFailingTransferRepo{TransferRepository: repos.Transfers}
	faultySvc := service.NewTransferService(postgres.NewTxManager(nil), faultyRepos)

	// Attempting stale recovery must fail fast with the lookup error rather than reclaiming
	_, err := faultySvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: idemKey,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	})
	if err == nil || !strings.Contains(err.Error(), "failed to lookup transfer during stale recovery") {
		t.Fatalf("expected error containing 'failed to lookup transfer during stale recovery', got %v", err)
	}

	// Reservation must still exist in IN_PROGRESS
	stillRec, _ := repos.Idempotency.GetIdempotency(ctx, idemKey)
	if stillRec == nil || stillRec.Status != domain.IdempotencyStatusInProgress {
		t.Fatalf("expected reservation to remain intact as IN_PROGRESS after lookup error, got %+v", stillRec)
	}
}

func TestTransferService_CleanupStaleReservation_LookupError_RetainsReservation(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, repos := setupServicesWithRepos(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_clean_err_1", Name: "W1", InitialBalance: 500})

	idemKey := "key_cleanup_err_" + uuid.NewString()
	requestHash := domain.ComputeRequestHash(w1.ID, "w_clean_err_missing", 100)

	// Seed in-progress reservation
	_, _, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		RequestHash:    requestHash,
		OwnerToken:     uuid.NewString(),
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil {
		t.Fatalf("failed to reserve idempotency: %v", err)
	}

	// Wrap transfer repo with simulated lookup failure
	faultyRepos := repos
	faultyRepos.Transfers = &lookupFailingTransferRepo{TransferRepository: repos.Transfers}
	faultySvc := service.NewTransferService(postgres.NewTxManager(nil), faultyRepos)

	// Execute with invalid payload (missing destination wallet ID) to trigger preflight failure
	_, err = faultySvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: idemKey,
		FromWalletID:   w1.ID,
		ToWalletID:     "", // missing wallet ID triggers checkValidationErr
		Amount:         100,
	})
	if err == nil {
		t.Fatalf("expected validation error, got nil")
	}

	// Reservation must NOT have been deleted because lookup encountered an operational error
	stillRec, _ := repos.Idempotency.GetIdempotency(ctx, idemKey)
	if stillRec == nil {
		t.Fatalf("reservation was incorrectly deleted despite operational error in transfer lookup")
	}
}

type blockingTxManager struct {
	realTxManager repository.TxManager
	onEnter       func()
}

func (m *blockingTxManager) ExecuteInTx(ctx context.Context, fn func(txRepos repository.Repositories) error) error {
	return m.realTxManager.ExecuteInTx(ctx, func(txRepos repository.Repositories) error {
		if m.onEnter != nil {
			m.onEnter()
		}
		return fn(txRepos)
	})
}

func TestTransferService_TxSemaphore_PreservesHeadroom(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, repos := setupServicesWithPool(t)
	txManager := postgres.NewTxManager(pool)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_sem_1", Name: "W1", InitialBalance: 500})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_sem_2", Name: "W2", InitialBalance: 500})

	idemKey1 := "key_sem_1_" + uuid.NewString()
	idemKey2 := "key_sem_2_" + uuid.NewString()

	enteredTx := make(chan struct{})
	releaseTx := make(chan struct{})

	blockingTx := &blockingTxManager{
		realTxManager: txManager,
		onEnter: func() {
			close(enteredTx)
			<-releaseTx
		},
	}
	blockingSvc := service.NewTransferServiceWithCapacity(blockingTx, repos, 1)

	go func() {
		_, _ = blockingSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
			IdempotencyKey: idemKey1,
			FromWalletID:   w1.ID,
			ToWalletID:     w2.ID,
			Amount:         10,
		})
	}()

	// Wait until the first transfer has entered the transaction and is holding the slot
	select {
	case <-enteredTx:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for first transfer to enter tx")
	}

	// A second transfer attempt with a short timeout must block on the semaphore and time out
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := blockingSvc.ExecuteTransfer(timeoutCtx, service.CreateTransferRequest{
		IdempotencyKey: idemKey2,
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         20,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected second transfer to time out waiting for transaction semaphore, got %v", err)
	}

	// Meanwhile, non-transactional database operations (like heartbeats and reads) must still succeed freely!
	w1Check, err := repos.Wallets.GetWalletByID(ctx, w1.ID)
	if err != nil || w1Check == nil {
		t.Fatalf("expected non-transactional read to succeed despite in-flight transaction, got %v", err)
	}

	// Release first transfer
	close(releaseTx)
}

func TestDatabaseTriggers_RejectTerminalTransferStatusMutation(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, _ := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_trig_term_1", Name: "W1", InitialBalance: 100})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_trig_term_2", Name: "W2", InitialBalance: 100})

	txID := "tx_term_" + uuid.NewString()

	// 1. Insert a transfer with PROCESSED status and matching ledger entries
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status) VALUES ($1, $2, $3, $4, $5, 'PROCESSED')",
		txID, "key_term_"+uuid.NewString(), w1.ID, w2.ID, 50)
	if err != nil {
		t.Fatalf("failed to insert processed transfer: %v", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount) VALUES ($1, $2, $3, 'DEBIT', 50), ($4, $2, $5, 'CREDIT', 50)",
		uuid.NewString(), txID, w1.ID, uuid.NewString(), w2.ID)
	if err != nil {
		t.Fatalf("failed to insert ledger entries: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("failed to commit valid processed transfer: %v", err)
	}

	// 2. Direct UPDATE transfers SET status = 'PENDING' on the already processed transfer must be rejected at database boundary!
	updateTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin update tx: %v", err)
	}
	defer updateTx.Rollback(ctx)

	_, err = updateTx.Exec(ctx, "UPDATE transfers SET status = 'PENDING' WHERE id = $1", txID)
	if err != nil {
		t.Fatalf("failed to execute update statement: %v", err)
	}

	commitErr := updateTx.Commit(ctx)
	if commitErr == nil {
		t.Fatalf("expected commit to fail when mutating terminal transfer status to PENDING, but got nil")
	}
	if !strings.Contains(commitErr.Error(), "is immutable and cannot be updated") {
		t.Fatalf("expected error message to contain 'is immutable and cannot be updated', got: %v", commitErr)
	}

	// 3. Direct UPDATE on non-status fields (amount, failure_reason) of a terminal transfer must ALSO be rejected
	amountTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin amount update tx: %v", err)
	}
	defer amountTx.Rollback(ctx)

	_, err = amountTx.Exec(ctx, "UPDATE transfers SET amount = 99999 WHERE id = $1", txID)
	if err != nil {
		t.Fatalf("failed to execute amount update statement: %v", err)
	}
	amountCommitErr := amountTx.Commit(ctx)
	if amountCommitErr == nil {
		t.Fatalf("expected commit to fail when mutating amount on terminal transfer, but got nil")
	}
	if !strings.Contains(amountCommitErr.Error(), "is immutable and cannot be updated") {
		t.Fatalf("expected error message to contain 'is immutable and cannot be updated', got: %v", amountCommitErr)
	}
}

func TestDatabaseTriggers_RejectLedgerEntriesForPendingTransfer(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, _ := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_trig_pend_1", Name: "W1", InitialBalance: 100})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_trig_pend_2", Name: "W2", InitialBalance: 100})

	txID := "tx_pend_" + uuid.NewString()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("failed to begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	// Insert PENDING transfer
	_, err = tx.Exec(ctx, "INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status) VALUES ($1, $2, $3, $4, $5, 'PENDING')",
		txID, "key_pend_"+uuid.NewString(), w1.ID, w2.ID, 30)
	if err != nil {
		t.Fatalf("failed to insert pending transfer: %v", err)
	}

	// Attach ledger entries to PENDING transfer
	_, err = tx.Exec(ctx, "INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount) VALUES ($1, $2, $3, 'DEBIT', 30), ($4, $2, $5, 'CREDIT', 30)",
		uuid.NewString(), txID, w1.ID, uuid.NewString(), w2.ID)
	if err != nil {
		t.Fatalf("failed to insert ledger entries: %v", err)
	}

	// Commit must fail because PENDING transfers cannot have ledger entries attached
	commitErr := tx.Commit(ctx)
	if commitErr == nil {
		t.Fatalf("expected commit to fail when ledger entries exist on PENDING transfer, but got nil")
	}
	if !strings.Contains(commitErr.Error(), "cannot commit non-terminal transfer") {
		t.Fatalf("expected error message to contain 'cannot commit non-terminal transfer', got: %v", commitErr)
	}
}

type trackingHeartbeatRepo struct {
	realRepo   repository.IdempotencyRepository
	heartbeats atomic.Int32
}

func (r *trackingHeartbeatRepo) ReserveIdempotency(ctx context.Context, record *domain.IdempotencyRecord, staleTimeout time.Duration) (*domain.IdempotencyRecord, bool, error) {
	return r.realRepo.ReserveIdempotency(ctx, record, staleTimeout)
}

func (r *trackingHeartbeatRepo) GetIdempotency(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	return r.realRepo.GetIdempotency(ctx, key)
}

func (r *trackingHeartbeatRepo) HeartbeatIdempotency(ctx context.Context, key string, ownerToken string) error {
	r.heartbeats.Add(1)
	return r.realRepo.HeartbeatIdempotency(ctx, key, ownerToken)
}

func (r *trackingHeartbeatRepo) UpdateIdempotency(ctx context.Context, record *domain.IdempotencyRecord) error {
	return r.realRepo.UpdateIdempotency(ctx, record)
}

func (r *trackingHeartbeatRepo) DeleteInProgress(ctx context.Context, key string, ownerToken string) error {
	return r.realRepo.DeleteInProgress(ctx, key, ownerToken)
}

func (r *trackingHeartbeatRepo) DeleteStaleInProgress(ctx context.Context, key string, ownerToken string, maxUpdatedAt time.Time) error {
	return r.realRepo.DeleteStaleInProgress(ctx, key, ownerToken, maxUpdatedAt)
}

func TestTransferService_DedicatedHeartbeatRepo(t *testing.T) {
	ctx := context.Background()
	_, walletSvc, pool, repos := setupServicesWithPool(t)

	w1, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_hb_1", Name: "W1", InitialBalance: 500})
	w2, _ := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_hb_2", Name: "W2", InitialBalance: 500})

	trackingHb := &trackingHeartbeatRepo{realRepo: repos.Idempotency}

	blockingTx := &blockingTxManager{
		realTxManager: postgres.NewTxManager(pool),
		onEnter: func() {
			time.Sleep(60 * time.Millisecond)
		},
	}
	customSvc := service.NewTransferServiceWithCapacity(blockingTx, repos, 5)
	customSvc.SetHeartbeatRepo(trackingHb)
	customSvc.SetHeartbeatInterval(20 * time.Millisecond)

	_, err := customSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "key_dedicated_hb_" + uuid.NewString(),
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         50,
	})
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	if trackingHb.heartbeats.Load() == 0 {
		t.Fatalf("expected dedicated heartbeat repository to receive heartbeats, got 0")
	}
}

func TestIdempotencyRepo_ReserveRetryOnConcurrentDeletion(t *testing.T) {
	ctx := context.Background()
	_, _, _, repos := setupServicesWithPool(t)

	idemKey := "key_disappear_" + uuid.NewString()
	token1 := uuid.NewString()

	// 1. Manually insert an initial reservation
	rec1 := &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		RequestHash:    "hash_test",
		OwnerToken:     token1,
		Status:         domain.IdempotencyStatusInProgress,
	}
	_, isOwner, err := repos.Idempotency.ReserveIdempotency(ctx, rec1, 30*time.Second)
	if err != nil || !isOwner {
		t.Fatalf("initial reservation failed: isOwner=%v, err=%v", isOwner, err)
	}

	// 2. Delete it concurrently
	if err := repos.Idempotency.DeleteInProgress(ctx, idemKey, token1); err != nil {
		t.Fatalf("failed to delete in-progress: %v", err)
	}

	// 3. Immediately reserve with a second token; it must succeed via retry rather than reporting disappearing row error
	token2 := uuid.NewString()
	rec2 := &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		RequestHash:    "hash_test",
		OwnerToken:     token2,
		Status:         domain.IdempotencyStatusInProgress,
	}
	res2, isOwner2, err2 := repos.Idempotency.ReserveIdempotency(ctx, rec2, 30*time.Second)
	if err2 != nil {
		t.Fatalf("expected reserve to succeed, got error: %v", err2)
	}
	if !isOwner2 || res2.OwnerToken != token2 {
		t.Fatalf("expected reserve to succeed as owner with token2, got isOwner=%v, owner=%s", isOwner2, res2.OwnerToken)
	}
}

func TestIdempotencyRepo_UpdateIdempotency_AlwaysSetsCurrentTimestamp(t *testing.T) {
	ctx := context.Background()
	_, _, repos := setupServicesWithRepos(t)

	idemKey := "key_update_ts_" + uuid.NewString()
	token := uuid.NewString()

	// 1. Reserve key
	rec, isOwner, err := repos.Idempotency.ReserveIdempotency(ctx, &domain.IdempotencyRecord{
		IdempotencyKey: idemKey,
		RequestHash:    "hash_test",
		OwnerToken:     token,
		Status:         domain.IdempotencyStatusInProgress,
	}, 30*time.Second)
	if err != nil || !isOwner {
		t.Fatalf("failed to reserve idempotency: %v", err)
	}
	initialUpdatedAt := rec.UpdatedAt

	// Sleep 50ms to ensure time elapses
	time.Sleep(50 * time.Millisecond)

	// 2. Finalize record passing the original record (which has initialUpdatedAt)
	rec.Status = domain.IdempotencyStatusCompleted
	rec.ResponseCode = 201
	rec.ResponseBody = `{"status":"ok"}`
	if err := repos.Idempotency.UpdateIdempotency(ctx, rec); err != nil {
		t.Fatalf("failed to update idempotency: %v", err)
	}

	// 3. In-memory struct must have been updated to a newer timestamp
	if !rec.UpdatedAt.After(initialUpdatedAt) {
		t.Fatalf("expected in-memory record.UpdatedAt (%v) to be after initial (%v)", rec.UpdatedAt, initialUpdatedAt)
	}

	// 4. Stored record in PostgreSQL must have the new current timestamp, not the initial reservation timestamp
	stored, err := repos.Idempotency.GetIdempotency(ctx, idemKey)
	if err != nil || stored == nil {
		t.Fatalf("failed to get idempotency: %v", err)
	}
	if !stored.UpdatedAt.After(initialUpdatedAt) {
		t.Fatalf("expected stored UpdatedAt (%v) to be after initial (%v)", stored.UpdatedAt, initialUpdatedAt)
	}
}
