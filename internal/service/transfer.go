package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"database/sql"
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

type idempotencyStore interface {
	Claim(ctx context.Context, key, requestHash string) (bool, error)
	Get(ctx context.Context, key string) (repository.IdempotencyRecord, error)
	Complete(ctx context.Context, key, requestHash, transferID string, status int, body []byte) error
}

type repositoryFactory func(repository.DBTX) (walletStore, transferStore, ledgerStore)

type TransferService struct {
	txManager TransactionManager
	newStores repositoryFactory
	newIdempotencyStore func(repository.DBTX) idempotencyStore
	newID func() (string, error)
}

func NewTransferService(manager TransactionManager) *TransferService {
	return &TransferService{
		txManager: manager,
		newStores: func(db repository.DBTX) (walletStore, transferStore, ledgerStore) {
			return repository.NewWalletRepository(db), repository.NewTransferRepository(db), repository.NewLedgerRepository(db)
		},
		newIdempotencyStore: func(db repository.DBTX) idempotencyStore {
			return repository.NewIdempotencyRepository(db)
		},
		newID: newID,
	}
}

type TransferRequest struct {
	FromWalletID string
	ToWalletID string
	Amount domain.Amount
}

type storedTransferResponse struct {
	Transfer domain.Transfer `json:"transfer"`
}

// Execute performs a transfer without an idempotency key. API callers should
// use ExecuteIdempotent so retries cannot create additional transfers.
func (s *TransferService) Execute(ctx context.Context, request TransferRequest) (domain.Transfer, error) {
	if err := validateRequest(request); err != nil { return domain.Transfer{}, err }
	tx, err := s.txManager.Begin(ctx)
	if err != nil { return domain.Transfer{}, fmt.Errorf("begin transfer transaction: %w", err) }
	defer func() { _ = tx.Rollback() }()
	transfer, operationErr := s.executeInTransaction(ctx, tx, request)
	if transfer.ID == "" { return domain.Transfer{}, operationErr }
	if err := tx.Commit(); err != nil { return domain.Transfer{}, fmt.Errorf("commit transfer transaction: %w", err) }
	return transfer, operationErr
}

// ExecuteIdempotent claims the key and performs the transfer in one transaction.
// A conflicting insert waits for the concurrent owner to commit or roll back;
// a subsequent SELECT at READ COMMITTED then observes the committed response.
func (s *TransferService) ExecuteIdempotent(ctx context.Context, key string, request TransferRequest) (domain.Transfer, error) {
	if strings.TrimSpace(key) == "" || len(key) > 255 {
		return domain.Transfer{}, ErrInvalidIdempotencyKey
	}
	if err := validateRequest(request); err != nil { return domain.Transfer{}, err }
	requestHash, err := hashRequest(request)
	if err != nil { return domain.Transfer{}, fmt.Errorf("hash transfer request: %w", err) }

	tx, err := s.txManager.Begin(ctx)
	if err != nil { return domain.Transfer{}, fmt.Errorf("begin idempotent transfer transaction: %w", err) }
	defer func() { _ = tx.Rollback() }()

	keys := s.newIdempotencyStore(tx)
	claimed, err := keys.Claim(ctx, key, requestHash)
	if err != nil { return domain.Transfer{}, fmt.Errorf("claim idempotency key: %w", err) }
	if !claimed {
		record, err := keys.Get(ctx, key)
		if err != nil { return domain.Transfer{}, fmt.Errorf("load idempotency record: %w", err) }
		if record.RequestHash != requestHash { return domain.Transfer{}, ErrIdempotencyConflict }
		if record.ResponseStatus == 0 || len(record.ResponseBody) == 0 {
			return domain.Transfer{}, ErrIncompleteIdempotencyRecord
		}
		var response storedTransferResponse
		if err := json.Unmarshal(record.ResponseBody, &response); err != nil {
			return domain.Transfer{}, fmt.Errorf("decode stored idempotency response: %w", err)
		}
		if err := response.Transfer.Validate(); err != nil {
			return domain.Transfer{}, fmt.Errorf("validate stored idempotency response: %w", err)
		}
		if err := tx.Commit(); err != nil { return domain.Transfer{}, fmt.Errorf("commit idempotent replay: %w", err) }
		return response.Transfer, businessErrorForTransfer(response.Transfer)
	}

	transfer, operationErr := s.executeInTransaction(ctx, tx, request)
	if transfer.ID == "" { return domain.Transfer{}, operationErr }
	status := 201
	if operationErr != nil { status = 422 }
	body, err := json.Marshal(storedTransferResponse{Transfer: transfer})
	if err != nil { return domain.Transfer{}, fmt.Errorf("encode idempotency response: %w", err) }
	if err := keys.Complete(ctx, key, requestHash, transfer.ID, status, body); err != nil {
		return domain.Transfer{}, fmt.Errorf("store idempotency response: %w", err)
	}
	if err := tx.Commit(); err != nil { return domain.Transfer{}, fmt.Errorf("commit idempotent transfer: %w", err) }
	return transfer, operationErr
}

func (s *TransferService) executeInTransaction(ctx context.Context, tx Transaction, request TransferRequest) (domain.Transfer, error) {
	wallets, transfers, ledger := s.newStores(tx)
	fromWallet, toWallet, err := lockWallets(ctx, wallets, request.FromWalletID, request.ToWalletID)
	if err != nil { return domain.Transfer{}, err }
	if err := fromWallet.Validate(); err != nil { return domain.Transfer{}, fmt.Errorf("invalid source wallet: %w", err) }
	if err := toWallet.Validate(); err != nil { return domain.Transfer{}, fmt.Errorf("invalid destination wallet: %w", err) }

	transferID, err := s.newID()
	if err != nil { return domain.Transfer{}, fmt.Errorf("generate transfer ID: %w", err) }
	transfer, err := domain.NewTransfer(transferID, request.FromWalletID, request.ToWalletID, request.Amount)
	if err != nil { return domain.Transfer{}, err }
	transfer, err = transfers.Create(ctx, transfer)
	if err != nil { return domain.Transfer{}, fmt.Errorf("persist pending transfer: %w", err) }

	switch {
	case fromWallet.Currency != toWallet.Currency:
		return s.markFailed(ctx, transfers, transfer.ID, failureCurrencyMismatch, domain.ErrCurrencyMismatch)
	case fromWallet.Balance < request.Amount:
		return s.markFailed(ctx, transfers, transfer.ID, failureInsufficientFunds, domain.ErrInsufficientFunds)
	}
	destinationAfterCredit := toWallet
	if err := destinationAfterCredit.Credit(request.Amount); err != nil {
		return s.markFailed(ctx, transfers, transfer.ID, failureBalanceOverflow, err)
	}
	if _, err := wallets.Debit(ctx, fromWallet.ID, request.Amount); err != nil { return domain.Transfer{}, fmt.Errorf("debit source wallet: %w", err) }
	if _, err := wallets.Credit(ctx, toWallet.ID, request.Amount); err != nil { return domain.Transfer{}, fmt.Errorf("credit destination wallet: %w", err) }

	debitID, err := s.newID()
	if err != nil { return domain.Transfer{}, fmt.Errorf("generate debit ledger ID: %w", err) }
	creditID, err := s.newID()
	if err != nil { return domain.Transfer{}, fmt.Errorf("generate credit ledger ID: %w", err) }
	debit := domain.LedgerEntry{ID:debitID, TransferID:transfer.ID, WalletID:fromWallet.ID, Type:domain.EntryDebit, Amount:request.Amount}
	credit := domain.LedgerEntry{ID:creditID, TransferID:transfer.ID, WalletID:toWallet.ID, Type:domain.EntryCredit, Amount:request.Amount}
	if err := domain.ValidateLedgerPair(debit, credit); err != nil { return domain.Transfer{}, fmt.Errorf("validate ledger pair: %w", err) }
	if _, err := ledger.Create(ctx, debit); err != nil { return domain.Transfer{}, fmt.Errorf("create debit ledger entry: %w", err) }
	if _, err := ledger.Create(ctx, credit); err != nil { return domain.Transfer{}, fmt.Errorf("create credit ledger entry: %w", err) }
	processed, err := transfers.MarkProcessed(ctx, transfer.ID)
	if err != nil { return domain.Transfer{}, fmt.Errorf("mark transfer processed: %w", err) }
	return processed, nil
}

func (s *TransferService) markFailed(ctx context.Context, transfers transferStore, id, code string, businessErr error) (domain.Transfer, error) {
	failed, err := transfers.MarkFailed(ctx, id, code)
	if err != nil { return domain.Transfer{}, fmt.Errorf("mark transfer failed: %w", err) }
	return failed, businessErr
}

func businessErrorForTransfer(transfer domain.Transfer) error {
	switch transfer.FailureCode {
	case failureInsufficientFunds: return domain.ErrInsufficientFunds
	case failureCurrencyMismatch: return domain.ErrCurrencyMismatch
	case failureBalanceOverflow: return domain.ErrBalanceOverflow
	default: return nil
	}
}

func validateRequest(request TransferRequest) error {
	if strings.TrimSpace(request.FromWalletID) == "" || strings.TrimSpace(request.ToWalletID) == "" { return domain.ErrInvalidWalletID }
	if request.FromWalletID == request.ToWalletID { return domain.ErrSameWallet }
	if request.Amount <= 0 { return domain.ErrInvalidAmount }
	return nil
}

func lockWallets(ctx context.Context, wallets walletStore, fromID, toID string) (domain.Wallet, domain.Wallet, error) {
	ids := []string{fromID, toID}
	sort.Strings(ids)
	locked := make(map[string]domain.Wallet, 2)
	for _, id := range ids {
		wallet, err := wallets.GetByIDForUpdate(ctx, id)
		if err != nil { return domain.Wallet{}, domain.Wallet{}, fmt.Errorf("lock wallet %q: %w", id, err) }
		locked[id] = wallet
	}
	return locked[fromID], locked[toID], nil
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil { return "", err }
	return hex.EncodeToString(value[:]), nil
}

type SQLTransactionManager struct {
	DB interface { BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error) }
}
func (m SQLTransactionManager) Begin(ctx context.Context) (Transaction, error) { return m.DB.BeginTx(ctx, nil) }
