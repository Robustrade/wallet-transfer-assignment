package transfer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/ledger"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/transfer"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/tx"
)

// --- Mocks ---

type mockTransferRepo struct {
	createFunc       func(ctx context.Context, t *transfer.Transfer) error
	getByIDFunc      func(ctx context.Context, id string) (*transfer.Transfer, error)
	getByTxnIDFunc   func(ctx context.Context, txnID string) (*transfer.Transfer, error)
	updateStatusFunc func(ctx context.Context, id string, status transfer.Status, failureReason string) error
}

func (m *mockTransferRepo) Create(ctx context.Context, t *transfer.Transfer) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, t)
	}
	return nil
}
func (m *mockTransferRepo) GetByID(ctx context.Context, id string) (*transfer.Transfer, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockTransferRepo) GetByTxnID(ctx context.Context, txnID string) (*transfer.Transfer, error) {
	if m.getByTxnIDFunc != nil {
		return m.getByTxnIDFunc(ctx, txnID)
	}
	return nil, nil
}
func (m *mockTransferRepo) UpdateStatus(ctx context.Context, id string, status transfer.Status, failureReason string) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, id, status, failureReason)
	}
	return nil
}

type mockWalletRepo struct {
	validateWalletsFunc func(ctx context.Context, ids []string) error
}

func (m *mockWalletRepo) ValidateWallets(ctx context.Context, ids []string) error {
	if m.validateWalletsFunc != nil {
		return m.validateWalletsFunc(ctx, ids)
	}
	return nil
}
func (m *mockWalletRepo) GetByID(ctx context.Context, id string) (*wallet.Wallet, error) { return nil, nil }
func (m *mockWalletRepo) Create(ctx context.Context, w *wallet.Wallet) error           { return nil }
func (m *mockWalletRepo) List(ctx context.Context, ownerID string) ([]*wallet.Wallet, error) { return nil, nil }


// Ledger mocks
type mockLedgerRepo struct{}
func (m *mockLedgerRepo) InsertDoubleEntry(ctx context.Context, debit, credit *ledger.Entry) error { return nil }
func (m *mockLedgerRepo) GetByWalletID(ctx context.Context, walletID string) ([]*ledger.Entry, error) { return nil, nil }

type mockBalanceRepo struct {
	debitAndCreditFunc func(ctx context.Context, fromID, toID string, amount int64) error
}
func (m *mockBalanceRepo) GetBalance(ctx context.Context, walletID string) (*ledger.Balance, error) { return nil, nil }
func (m *mockBalanceRepo) DebitAndCredit(ctx context.Context, fromID, toID string, amount int64) error {
	if m.debitAndCreditFunc != nil {
		return m.debitAndCreditFunc(ctx, fromID, toID, amount)
	}
	return nil
}

type mockTx struct{}
func (m *mockTx) Commit() error   { return nil }
func (m *mockTx) Rollback() error { return nil }

type mockTxStarter struct{}
func (m *mockTxStarter) BeginTx(ctx context.Context) (context.Context, tx.Tx, error) { return ctx, &mockTx{}, nil }


// --- Tests ---

func TestService_Execute(t *testing.T) {
	t.Run("Idempotency - PROCESSED returns immediately", func(t *testing.T) {
		existing := &transfer.Transfer{ID: "txn_123", Status: transfer.StatusProcessed}
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) {
				return existing, nil
			},
		}
		svc := transfer.NewService(repo, nil, nil)
		res, err := svc.Execute(context.Background(), &transfer.CreateRequest{TxnID: "idem_1"})

		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if res != existing {
			t.Errorf("expected existing transfer, got %v", res)
		}
	})

	t.Run("Idempotency - FAILED returns immediately", func(t *testing.T) {
		existing := &transfer.Transfer{ID: "txn_fail", Status: transfer.StatusFailed, FailureReason: "wallet inactive"}
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) {
				return existing, nil
			},
		}
		svc := transfer.NewService(repo, nil, nil)
		res, err := svc.Execute(context.Background(), &transfer.CreateRequest{TxnID: "idem_fail"})

		if err != nil {
			t.Errorf("expected nil error for terminal FAILED, got %v", err)
		}
		if res != existing {
			t.Errorf("expected existing FAILED transfer, got %v", res)
		}
	})

	t.Run("Idempotency - PENDING resumes workflow", func(t *testing.T) {
		// A prior attempt created a PENDING record but was interrupted.
		// Execute should resume and complete it to PROCESSED.
		pending := &transfer.Transfer{
			ID:           "txn_pending",
			TxnID:        "idem_resume",
			FromWalletID: "w1",
			ToWalletID:   "w2",
			Amount:       100,
			Status:       transfer.StatusPending,
		}
		statusUpdated := false
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) {
				return pending, nil
			},
			// Create must NOT be called — we're resuming an existing record.
			createFunc: func(ctx context.Context, tr *transfer.Transfer) error {
				t.Error("Create should not be called when resuming a PENDING transfer")
				return nil
			},
			updateStatusFunc: func(ctx context.Context, id string, status transfer.Status, reason string) error {
				statusUpdated = true
				if status != transfer.StatusProcessed {
					t.Errorf("expected status PROCESSED on resume, got %v", status)
				}
				return nil
			},
		}
		wRepo := &mockWalletRepo{
			validateWalletsFunc: func(ctx context.Context, ids []string) error { return nil },
		}
		ledgerSvc := ledger.NewService(&mockLedgerRepo{}, &mockBalanceRepo{}, &mockTxStarter{})

		svc := transfer.NewService(repo, wRepo, ledgerSvc)
		tr, err := svc.Execute(context.Background(), &transfer.CreateRequest{TxnID: "idem_resume"})

		if err != nil {
			t.Errorf("expected successful resume, got error %v", err)
		}
		if !statusUpdated {
			t.Error("expected status to be updated to PROCESSED in DB after resume")
		}
		if tr.Status != transfer.StatusProcessed {
			t.Errorf("expected transfer status PROCESSED after resume, got %v", tr.Status)
		}
	})

	t.Run("Creation Failure (Persist PENDING)", func(t *testing.T) {
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) {
				return nil, nil // not found
			},
			createFunc: func(ctx context.Context, tr *transfer.Transfer) error {
				return errors.New("db error")
			},
		}
		svc := transfer.NewService(repo, nil, nil)
		req := &transfer.CreateRequest{TxnID: "idem_2"}
		_, err := svc.Execute(context.Background(), req)
		if err == nil {
			t.Error("expected error on create failure, got nil")
		}
	})

	t.Run("Validation Failure (Entity)", func(t *testing.T) {
		statusUpdated := false
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) { return nil, nil },
			createFunc:     func(ctx context.Context, tr *transfer.Transfer) error { return nil },
			updateStatusFunc: func(ctx context.Context, id string, status transfer.Status, reason string) error {
				statusUpdated = true
				if status != transfer.StatusFailed {
					t.Errorf("expected status FAILED, got %v", status)
				}
				return nil
			},
		}
		svc := transfer.NewService(repo, nil, nil)
		// invalid request (amount <= 0)
		req := &transfer.CreateRequest{TxnID: "idem_3", FromWalletID: "w1", ToWalletID: "w2", Amount: -10}
		tr, err := svc.Execute(context.Background(), req)
		
		if err == nil {
			t.Error("expected error for invalid entity")
		}
		if !statusUpdated {
			t.Error("expected status to be updated to FAILED in DB")
		}
		if tr.Status != transfer.StatusFailed {
			t.Errorf("expected transfer status FAILED in memory, got %v", tr.Status)
		}
	})

	t.Run("Validation Failure (Wallets)", func(t *testing.T) {
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) { return nil, nil },
			createFunc:     func(ctx context.Context, tr *transfer.Transfer) error { return nil },
			updateStatusFunc: func(ctx context.Context, id string, status transfer.Status, reason string) error {
				return nil
			},
		}
		wRepo := &mockWalletRepo{
			validateWalletsFunc: func(ctx context.Context, ids []string) error {
				return errors.New("wallet inactive")
			},
		}
		svc := transfer.NewService(repo, wRepo, nil)
		req := &transfer.CreateRequest{TxnID: "idem_4", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}
		tr, err := svc.Execute(context.Background(), req)
		
		if err == nil {
			t.Error("expected error for wallet validation failure")
		}
		if tr.Status != transfer.StatusFailed {
			t.Errorf("expected transfer status FAILED, got %v", tr.Status)
		}
	})

	t.Run("Ledger Failure - InsufficientBalance leaves PENDING (retryable)", func(t *testing.T) {
		// ErrInsufficientBalance is retryable — balance state can change between attempts.
		// The transfer must remain PENDING; UpdateStatus must NOT be called.
		repo := &mockTransferRepo{
			getByTxnIDFunc:   func(ctx context.Context, txnID string) (*transfer.Transfer, error) { return nil, nil },
			createFunc:       func(ctx context.Context, tr *transfer.Transfer) error { return nil },
			updateStatusFunc: func(ctx context.Context, id string, status transfer.Status, reason string) error {
				t.Errorf("UpdateStatus must not be called for retryable error, got status %v", status)
				return nil
			},
		}
		wRepo := &mockWalletRepo{
			validateWalletsFunc: func(ctx context.Context, ids []string) error { return nil },
		}
		bRepo := &mockBalanceRepo{
			debitAndCreditFunc: func(ctx context.Context, from, to string, amt int64) error {
				return ledger.ErrInsufficientBalance
			},
		}
		ledgerSvc := ledger.NewService(&mockLedgerRepo{}, bRepo, &mockTxStarter{})

		svc := transfer.NewService(repo, wRepo, ledgerSvc)
		req := &transfer.CreateRequest{TxnID: "idem_5", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}
		tr, err := svc.Execute(context.Background(), req)

		if err == nil {
			t.Error("expected error for insufficient balance")
		}
		if !errors.Is(err, ledger.ErrInsufficientBalance) {
			t.Errorf("expected wrapped ErrInsufficientBalance in error chain, got: %v", err)
		}
		// Transfer must remain PENDING — balance can change; the caller should retry.
		if tr.Status != transfer.StatusPending {
			t.Errorf("expected transfer to remain PENDING for retryable error, got %v", tr.Status)
		}
	})

	t.Run("Ledger Failure - Technical error leaves PENDING (retryable)", func(t *testing.T) {
		// A non-business error from the ledger (e.g. DB connectivity, tx abort)
		// must leave the transfer PENDING so the caller can retry.
		// UpdateStatus must NOT be called — the record stays in PENDING.
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) { return nil, nil },
			createFunc:     func(ctx context.Context, tr *transfer.Transfer) error { return nil },
			updateStatusFunc: func(ctx context.Context, id string, status transfer.Status, reason string) error {
				t.Errorf("UpdateStatus must not be called for retryable error, got status %v", status)
				return nil
			},
		}
		wRepo := &mockWalletRepo{
			validateWalletsFunc: func(ctx context.Context, ids []string) error { return nil },
		}
		// Simulate a technical DB failure (not a business rule violation)
		techErr := errors.New("connection reset by peer")
		bRepo := &mockBalanceRepo{
			debitAndCreditFunc: func(ctx context.Context, from, to string, amt int64) error {
				return techErr
			},
		}
		ledgerSvc := ledger.NewService(&mockLedgerRepo{}, bRepo, &mockTxStarter{})

		svc := transfer.NewService(repo, wRepo, ledgerSvc)
		req := &transfer.CreateRequest{TxnID: "idem_retry", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}
		tr, err := svc.Execute(context.Background(), req)

		if err == nil {
			t.Error("expected error for technical ledger failure")
		}
		// Error message must signal retryability
		if !errors.Is(err, techErr) {
			t.Errorf("expected wrapped techErr in error chain, got: %v", err)
		}
		// Transfer must remain PENDING — not FAILED
		if tr.Status != transfer.StatusPending {
			t.Errorf("expected transfer to remain PENDING for retryable error, got %v", tr.Status)
		}
	})


	t.Run("Success", func(t *testing.T) {
		statusUpdated := false
		repo := &mockTransferRepo{
			getByTxnIDFunc: func(ctx context.Context, txnID string) (*transfer.Transfer, error) { return nil, nil },
			createFunc:     func(ctx context.Context, tr *transfer.Transfer) error { return nil },
			updateStatusFunc: func(ctx context.Context, id string, status transfer.Status, reason string) error {
				statusUpdated = true
				if status != transfer.StatusProcessed {
					t.Errorf("expected status PROCESSED, got %v", status)
				}
				return nil
			},
		}
		wRepo := &mockWalletRepo{
			validateWalletsFunc: func(ctx context.Context, ids []string) error { return nil },
		}
		ledgerSvc := ledger.NewService(&mockLedgerRepo{}, &mockBalanceRepo{}, &mockTxStarter{})

		svc := transfer.NewService(repo, wRepo, ledgerSvc)
		req := &transfer.CreateRequest{TxnID: "idem_6", FromWalletID: "w1", ToWalletID: "w2", Amount: 100}
		tr, err := svc.Execute(context.Background(), req)
		
		if err != nil {
			t.Errorf("expected success, got error %v", err)
		}
		if !statusUpdated {
			t.Error("expected status to be updated to PROCESSED in DB")
		}
		if tr.Status != transfer.StatusProcessed {
			t.Errorf("expected transfer status PROCESSED, got %v", tr.Status)
		}
	})
}
