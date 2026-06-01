package ledger_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/ledger"
	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/tx"
)

// --- Mocks ---

type mockLedgerRepo struct {
	insertDoubleEntryFunc func(ctx context.Context, debit, credit *ledger.Entry) error
	getByWalletIDFunc     func(ctx context.Context, walletID string) ([]*ledger.Entry, error)
}

func (m *mockLedgerRepo) InsertDoubleEntry(ctx context.Context, debit, credit *ledger.Entry) error {
	if m.insertDoubleEntryFunc != nil {
		return m.insertDoubleEntryFunc(ctx, debit, credit)
	}
	return nil
}

func (m *mockLedgerRepo) GetByWalletID(ctx context.Context, walletID string) ([]*ledger.Entry, error) {
	if m.getByWalletIDFunc != nil {
		return m.getByWalletIDFunc(ctx, walletID)
	}
	return nil, nil
}

type mockBalanceRepo struct {
	getBalanceFunc     func(ctx context.Context, walletID string) (*ledger.Balance, error)
	debitAndCreditFunc func(ctx context.Context, fromID, toID string, amount int64) error
}

func (m *mockBalanceRepo) GetBalance(ctx context.Context, walletID string) (*ledger.Balance, error) {
	if m.getBalanceFunc != nil {
		return m.getBalanceFunc(ctx, walletID)
	}
	return nil, nil
}

func (m *mockBalanceRepo) DebitAndCredit(ctx context.Context, fromID, toID string, amount int64) error {
	if m.debitAndCreditFunc != nil {
		return m.debitAndCreditFunc(ctx, fromID, toID, amount)
	}
	return nil
}

type mockTx struct {
	commitFunc   func() error
	rollbackFunc func() error
	committed    bool
	rolledBack   bool
}

func (m *mockTx) Commit() error {
	m.committed = true
	if m.commitFunc != nil {
		return m.commitFunc()
	}
	return nil
}

func (m *mockTx) Rollback() error {
	m.rolledBack = true
	if m.rollbackFunc != nil {
		return m.rollbackFunc()
	}
	return nil
}

type mockTxStarter struct {
	beginTxFunc func(ctx context.Context) (context.Context, tx.Tx, error)
}

func (m *mockTxStarter) BeginTx(ctx context.Context) (context.Context, tx.Tx, error) {
	if m.beginTxFunc != nil {
		return m.beginTxFunc(ctx)
	}
	return ctx, &mockTx{}, nil
}

// --- Tests ---

func TestService_GetBalance(t *testing.T) {
	mockBal := &ledger.Balance{WalletID: "w1", Amount: 100}
	
	t.Run("Success", func(t *testing.T) {
		repo := &mockBalanceRepo{
			getBalanceFunc: func(ctx context.Context, walletID string) (*ledger.Balance, error) {
				return mockBal, nil
			},
		}
		svc := ledger.NewService(&mockLedgerRepo{}, repo, &mockTxStarter{})
		res, err := svc.GetBalance(context.Background(), "w1")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if res != mockBal {
			t.Errorf("expected mock balance, got %v", res)
		}
	})

	t.Run("Failure", func(t *testing.T) {
		repo := &mockBalanceRepo{
			getBalanceFunc: func(ctx context.Context, walletID string) (*ledger.Balance, error) {
				return nil, ledger.ErrWalletNotFound
			},
		}
		svc := ledger.NewService(&mockLedgerRepo{}, repo, &mockTxStarter{})
		_, err := svc.GetBalance(context.Background(), "w1")
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}

func TestService_CommitToLedger(t *testing.T) {
	t.Run("Transaction Begin Failure", func(t *testing.T) {
		db := &mockTxStarter{
			beginTxFunc: func(ctx context.Context) (context.Context, tx.Tx, error) {
				return ctx, nil, errors.New("db down")
			},
		}
		svc := ledger.NewService(&mockLedgerRepo{}, &mockBalanceRepo{}, db)
		err := svc.CommitToLedger(context.Background(), "t1", "w1", "w2", 100)
		if err == nil {
			t.Error("expected error for begin tx failure")
		}
	})

	t.Run("Balance Update Failure", func(t *testing.T) {
		txMock := &mockTx{}
		db := &mockTxStarter{
			beginTxFunc: func(ctx context.Context) (context.Context, tx.Tx, error) { return ctx, txMock, nil },
		}
		bRepo := &mockBalanceRepo{
			debitAndCreditFunc: func(ctx context.Context, f, t string, amt int64) error {
				return ledger.ErrInsufficientBalance
			},
		}
		svc := ledger.NewService(&mockLedgerRepo{}, bRepo, db)
		err := svc.CommitToLedger(context.Background(), "t1", "w1", "w2", 100)
		if err == nil {
			t.Error("expected error for balance update failure")
		}
		if !txMock.rolledBack {
			t.Error("expected tx to be rolled back")
		}
	})

	t.Run("Ledger Entry Failure", func(t *testing.T) {
		txMock := &mockTx{}
		db := &mockTxStarter{
			beginTxFunc: func(ctx context.Context) (context.Context, tx.Tx, error) { return ctx, txMock, nil },
		}
		lRepo := &mockLedgerRepo{
			insertDoubleEntryFunc: func(ctx context.Context, debit, credit *ledger.Entry) error {
				return errors.New("duplicate entry")
			},
		}
		svc := ledger.NewService(lRepo, &mockBalanceRepo{}, db)
		err := svc.CommitToLedger(context.Background(), "t1", "w1", "w2", 100)
		if err == nil {
			t.Error("expected error for ledger entry failure")
		}
		if !txMock.rolledBack {
			t.Error("expected tx to be rolled back")
		}
	})

	t.Run("Commit Failure", func(t *testing.T) {
		txMock := &mockTx{
			commitFunc: func() error { return errors.New("commit failed") },
		}
		db := &mockTxStarter{
			beginTxFunc: func(ctx context.Context) (context.Context, tx.Tx, error) { return ctx, txMock, nil },
		}
		svc := ledger.NewService(&mockLedgerRepo{}, &mockBalanceRepo{}, db)
		err := svc.CommitToLedger(context.Background(), "t1", "w1", "w2", 100)
		if err == nil {
			t.Error("expected error for commit failure")
		}
		if !txMock.rolledBack {
			t.Error("expected tx to be rolled back after failed commit")
		}
	})

	t.Run("Success", func(t *testing.T) {
		txMock := &mockTx{}
		db := &mockTxStarter{
			beginTxFunc: func(ctx context.Context) (context.Context, tx.Tx, error) { return ctx, txMock, nil },
		}
		
		var debitEntry, creditEntry *ledger.Entry
		lRepo := &mockLedgerRepo{
			insertDoubleEntryFunc: func(ctx context.Context, d, c *ledger.Entry) error {
				debitEntry = d
				creditEntry = c
				return nil
			},
		}
		
		svc := ledger.NewService(lRepo, &mockBalanceRepo{}, db)
		err := svc.CommitToLedger(context.Background(), "t1", "w1", "w2", 100)
		if err != nil {
			t.Errorf("expected success, got error %v", err)
		}
		if !txMock.committed {
			t.Error("expected tx to be committed")
		}
		if txMock.rolledBack {
			t.Error("did not expect tx to be rolled back")
		}
		if debitEntry == nil || debitEntry.Type != ledger.EntryTypeDebit {
			t.Error("expected debit entry to be inserted")
		}
		if creditEntry == nil || creditEntry.Type != ledger.EntryTypeCredit {
			t.Error("expected credit entry to be inserted")
		}
	})
}
