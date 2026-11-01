package service

import (
	"context"
	"fmt"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type fakeStore struct {
	wallets    map[string]domain.Wallet
	idem       map[string]repository.IdempotencyRecord
	transfers  map[string]domain.Transfer
	ledger     []domain.LedgerEntry
	nextID     int
	nextLedger int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		wallets:   map[string]domain.Wallet{},
		idem:      map[string]repository.IdempotencyRecord{},
		transfers: map[string]domain.Transfer{},
	}
}

type fakeSnap struct {
	wallets    map[string]domain.Wallet
	idem       map[string]repository.IdempotencyRecord
	transfers  map[string]domain.Transfer
	ledger     []domain.LedgerEntry
	nextID     int
	nextLedger int64
}

func (s *fakeStore) WithTx(ctx context.Context, fn func(tx repository.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	shot := s.clone()
	if err := fn(&fakeTx{s: s}); err != nil {
		s.restore(shot)
		return err
	}
	return nil
}

func (s *fakeStore) CreateWallet(_ context.Context, wallet domain.Wallet) error {
	if _, ok := s.wallets[wallet.ID]; ok {
		return domain.ErrAlreadyExists
	}
	s.wallets[wallet.ID] = wallet
	return nil
}

func (s *fakeStore) GetWallet(_ context.Context, id string) (domain.Wallet, error) {
	wallet, ok := s.wallets[id]
	if !ok {
		return domain.Wallet{}, domain.ErrNotFound
	}
	return wallet, nil
}

func (s *fakeStore) GetTransfer(_ context.Context, id string) (domain.Transfer, []domain.LedgerEntry, error) {
	transfer, ok := s.transfers[id]
	if !ok {
		return domain.Transfer{}, nil, domain.ErrNotFound
	}
	entries := make([]domain.LedgerEntry, 0, 2)
	for _, entry := range s.ledger {
		if entry.TransferID == id {
			entries = append(entries, entry)
		}
	}
	return transfer, entries, nil
}

func (s *fakeStore) clone() fakeSnap {
	shot := fakeSnap{
		wallets:    make(map[string]domain.Wallet, len(s.wallets)),
		idem:       make(map[string]repository.IdempotencyRecord, len(s.idem)),
		transfers:  make(map[string]domain.Transfer, len(s.transfers)),
		ledger:     append([]domain.LedgerEntry(nil), s.ledger...),
		nextID:     s.nextID,
		nextLedger: s.nextLedger,
	}
	for id, wallet := range s.wallets {
		shot.wallets[id] = wallet
	}
	for key, rec := range s.idem {
		rec.ResponseBody = append([]byte(nil), rec.ResponseBody...)
		shot.idem[key] = rec
	}
	for id, transfer := range s.transfers {
		shot.transfers[id] = transfer
	}
	return shot
}

func (s *fakeStore) restore(shot fakeSnap) {
	s.wallets = shot.wallets
	s.idem = shot.idem
	s.transfers = shot.transfers
	s.ledger = shot.ledger
	s.nextID = shot.nextID
	s.nextLedger = shot.nextLedger
}

type fakeTx struct {
	s *fakeStore
}

func (t *fakeTx) ClaimIdempotency(_ context.Context, key, requestHash string) (repository.IdempotencyRecord, bool, error) {
	if rec, ok := t.s.idem[key]; ok {
		rec.ResponseBody = append([]byte(nil), rec.ResponseBody...)
		return rec, false, nil
	}
	rec := repository.IdempotencyRecord{Key: key, RequestHash: requestHash}
	t.s.idem[key] = rec
	return rec, true, nil
}

func (t *fakeTx) LockIdempotency(_ context.Context, key string) (repository.IdempotencyRecord, bool, error) {
	rec, ok := t.s.idem[key]
	if !ok {
		return repository.IdempotencyRecord{}, false, nil
	}
	rec.ResponseBody = append([]byte(nil), rec.ResponseBody...)
	return rec, true, nil
}

func (t *fakeTx) SaveIdempotencyResult(_ context.Context, key string, status int, body []byte, transferID string) error {
	rec, ok := t.s.idem[key]
	if !ok {
		return fmt.Errorf("idempotency key %s is not claimed", key)
	}
	if rec.ResponseStatus != 0 {
		return fmt.Errorf("idempotency key %s already has a result", key)
	}
	rec.ResponseStatus = status
	rec.ResponseBody = append([]byte(nil), body...)
	rec.TransferID = transferID
	t.s.idem[key] = rec
	return nil
}

func (t *fakeTx) LockWallets(_ context.Context, ids ...string) (map[string]domain.Wallet, error) {
	wallets := make(map[string]domain.Wallet, len(ids))
	for _, id := range ids {
		if wallet, ok := t.s.wallets[id]; ok {
			wallets[id] = wallet
		}
	}
	return wallets, nil
}

func (t *fakeTx) InsertTransfer(_ context.Context, transfer domain.Transfer) (string, error) {
	for _, existing := range t.s.transfers {
		if existing.IdempotencyKey == transfer.IdempotencyKey {
			return "", fmt.Errorf("duplicate idempotency key %s", transfer.IdempotencyKey)
		}
	}
	t.s.nextID++
	transfer.ID = fmt.Sprintf("00000000-0000-4000-8000-%012d", t.s.nextID)
	transfer.Status = domain.StatusPending
	t.s.transfers[transfer.ID] = transfer
	return transfer.ID, nil
}

func (t *fakeTx) UpdateTransferStatus(_ context.Context, id string, from, to domain.Status, reason string) error {
	if !domain.CanTransition(from, to) {
		return fmt.Errorf("illegal transfer transition %s to %s", from, to)
	}
	current, ok := t.s.transfers[id]
	if !ok || current.Status != from {
		return fmt.Errorf("transfer %s status update matched no row", id)
	}
	current.Status = to
	current.FailureReason = reason
	t.s.transfers[id] = current
	return nil
}

func (t *fakeTx) InsertLedgerPair(_ context.Context, transferID, fromWalletID, toWalletID string, amount int64) error {
	for _, entry := range t.s.ledger {
		if entry.TransferID == transferID {
			return fmt.Errorf("transfer %s already has ledger entries", transferID)
		}
	}
	t.s.nextLedger++
	debitID := t.s.nextLedger
	t.s.nextLedger++
	creditID := t.s.nextLedger
	t.s.ledger = append(t.s.ledger,
		domain.LedgerEntry{ID: debitID, WalletID: fromWalletID, TransferID: transferID, Type: domain.Debit, Amount: amount},
		domain.LedgerEntry{ID: creditID, WalletID: toWalletID, TransferID: transferID, Type: domain.Credit, Amount: amount},
	)
	return nil
}

func (t *fakeTx) DebitWallet(_ context.Context, walletID string, amount int64) error {
	wallet, ok := t.s.wallets[walletID]
	if !ok {
		return domain.ErrNotFound
	}
	if wallet.Balance < amount {
		return domain.ErrInsufficientFunds
	}
	wallet.Balance -= amount
	t.s.wallets[walletID] = wallet
	return nil
}

func (t *fakeTx) CreditWallet(_ context.Context, walletID string, amount int64) error {
	wallet, ok := t.s.wallets[walletID]
	if !ok {
		return domain.ErrNotFound
	}
	if domain.CreditOverflow(wallet.Balance, amount) {
		return fmt.Errorf("credit overflow")
	}
	wallet.Balance += amount
	t.s.wallets[walletID] = wallet
	return nil
}
