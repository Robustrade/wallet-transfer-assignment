package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/abhiabhi0/wallet-transfer-assignment/internal/domain"
	"github.com/abhiabhi0/wallet-transfer-assignment/internal/repository"
)

const (
	failureInsufficientFunds = "INSUFFICIENT_FUNDS"
	failureCurrencyMismatch  = "CURRENCY_MISMATCH"
	failureBalanceOverflow   = "BALANCE_OVERFLOW"
)

type Transaction interface {
	repository.DBTX
	Commit() error
	Rollback() error
}

type TransactionManager interface {
	Begin(ctx context.Context) (Transaction, error)
}

type walletStore interface {
	GetByIDForUpdate(ctx context.Context, id string) (domain.Wallet, error)
	Debit(ctx context.Context, id string, amount domain.Amount) (domain.Wallet, error)
	Credit(ctx context.Context, id string, amount domain.Amount) (domain.Wallet, error)
}

type transferStore interface {
	Create(ctx context.Context, transfer domain.Transfer) (domain.Transfer, error)
	MarkProcessed(ctx context.Context, id string) (domain.Transfer, error)
	MarkFailed(ctx context.Context, id, failureCode string) (domain.Transfer, error)
}

type ledgerStore interface {
	Create(ctx context.Context, entry domain.LedgerEntry) (domain.LedgerEntry, error)
}

type repositoryFactory func(repository.DBTX) (walletStore, transferStore, ledgerStore)

type TransferService struct {
	txManager TransactionManager
	newStores repositoryFactory
	newID     func() (string, error)
}

// NewTransferService wires the service to a transaction manager. Use
// SQLTransactionManager{DB: db} with a PostgreSQL *sql.DB in the application.
func NewTransferService(manager TransactionManager) *TransferService {
	return &TransferService{
		txManager: manager,
		newStores: func(db repository.DBTX) (walletStore, transferStore, ledgerStore) {
			return repository.NewWalletRepository(db), repository.NewTransferRepository(db), repository.NewLedgerRepository(db)
		},
		newID: newID,
	}
}

type TransferRequest struct {
	FromWalletID string
	ToWalletID   string
	Amount       domain.Amount
}

// Execute performs a synchronous transfer. Deterministic business failures are
// persisted as FAILED transfers and returned alongside their domain error.
// Infrastructure failures roll back the complete transaction.
func (s *TransferService) Execute(ctx context.Context, request TransferRequest) (domain.Transfer, error) {
	if err := validateRequest(request); err != nil {
		return domain.Transfer{}, err
	}

	tx, err := s.txManager.Begin(ctx)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("begin transfer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	wallets, transfers, ledger := s.newStores(tx)
	fromWallet, toWallet, err := lockWallets(ctx, wallets, request.FromWalletID, request.ToWalletID)
	if err != nil {
		return domain.Transfer{}, err
	}
	if err := fromWallet.Validate(); err != nil {
		return domain.Transfer{}, fmt.Errorf("invalid source wallet: %w", err)
	}
	if err := toWallet.Validate(); err != nil {
		return domain.Transfer{}, fmt.Errorf("invalid destination wallet: %w", err)
	}

	transferID, err := s.newID()
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("generate transfer ID: %w", err)
	}
	transfer, err := domain.NewTransfer(transferID, request.FromWalletID, request.ToWalletID, request.Amount)
	if err != nil {
		return domain.Transfer{}, err
	}
	transfer, err = transfers.Create(ctx, transfer)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("persist pending transfer: %w", err)
	}

	switch {
	case fromWallet.Currency != toWallet.Currency:
		return s.failAndCommit(ctx, tx, transfers, transfer.ID, failureCurrencyMismatch, domain.ErrCurrencyMismatch)
	case fromWallet.Balance < request.Amount:
		return s.failAndCommit(ctx, tx, transfers, transfer.ID, failureInsufficientFunds, domain.ErrInsufficientFunds)
	}

	// Check destination overflow while both wallet rows are locked and before
	// mutating either balance.
	destinationAfterCredit := toWallet
	if err := destinationAfterCredit.Credit(request.Amount); err != nil {
		return s.failAndCommit(ctx, tx, transfers, transfer.ID, failureBalanceOverflow, err)
	}

	if _, err := wallets.Debit(ctx, fromWallet.ID, request.Amount); err != nil {
		return domain.Transfer{}, fmt.Errorf("debit source wallet: %w", err)
	}
	if _, err := wallets.Credit(ctx, toWallet.ID, request.Amount); err != nil {
		return domain.Transfer{}, fmt.Errorf("credit destination wallet: %w", err)
	}

	debitID, err := s.newID()
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("generate debit ledger ID: %w", err)
	}
	creditID, err := s.newID()
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("generate credit ledger ID: %w", err)
	}
	debit := domain.LedgerEntry{
		ID: debitID, TransferID: transfer.ID, WalletID: fromWallet.ID,
		Type: domain.EntryDebit, Amount: request.Amount,
	}
	credit := domain.LedgerEntry{
		ID: creditID, TransferID: transfer.ID, WalletID: toWallet.ID,
		Type: domain.EntryCredit, Amount: request.Amount,
	}
	if err := domain.ValidateLedgerPair(debit, credit); err != nil {
		return domain.Transfer{}, fmt.Errorf("validate ledger pair: %w", err)
	}
	if _, err := ledger.Create(ctx, debit); err != nil {
		return domain.Transfer{}, fmt.Errorf("create debit ledger entry: %w", err)
	}
	if _, err := ledger.Create(ctx, credit); err != nil {
		return domain.Transfer{}, fmt.Errorf("create credit ledger entry: %w", err)
	}

	processed, err := transfers.MarkProcessed(ctx, transfer.ID)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("mark transfer processed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Transfer{}, fmt.Errorf("commit transfer transaction: %w", err)
	}
	return processed, nil
}

func (s *TransferService) failAndCommit(
	ctx context.Context,
	tx Transaction,
	transfers transferStore,
	transferID, failureCode string,
	businessErr error,
) (domain.Transfer, error) {
	failed, err := transfers.MarkFailed(ctx, transferID, failureCode)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("mark transfer failed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.Transfer{}, fmt.Errorf("commit failed transfer: %w", err)
	}
	return failed, businessErr
}

func validateRequest(request TransferRequest) error {
	if strings.TrimSpace(request.FromWalletID) == "" || strings.TrimSpace(request.ToWalletID) == "" {
		return domain.ErrInvalidWalletID
	}
	if request.FromWalletID == request.ToWalletID {
		return domain.ErrSameWallet
	}
	if request.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	return nil
}

// lockWallets acquires FOR UPDATE locks in sorted ID order, preventing
// A-to-B and B-to-A transfers from taking the same pair of locks oppositely.
func lockWallets(ctx context.Context, wallets walletStore, fromID, toID string) (domain.Wallet, domain.Wallet, error) {
	ids := []string{fromID, toID}
	sort.Strings(ids)
	locked := make(map[string]domain.Wallet, 2)
	for _, id := range ids {
		wallet, err := wallets.GetByIDForUpdate(ctx, id)
		if err != nil {
			return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("lock wallet %q: %w", id, err)
		}
		locked[id] = wallet
	}
	return locked[fromID], locked[toID], nil
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

// SQLTransactionManager adapts database/sql's concrete transaction type.
type SQLTransactionManager struct {
	DB interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}
}

func (m SQLTransactionManager) Begin(ctx context.Context) (Transaction, error) {
	return m.DB.BeginTx(ctx, nil)
}
