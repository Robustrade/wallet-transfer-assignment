package service

import (
	"context"
	"errors"
	"testing"

	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"github.com/gerry1818/wallet-transfer-assignment/internal/repository"
	"github.com/jackc/pgx/v5"
)

type mockTx struct {
	wallets            map[int64]int64
	commitErr          error
	createTransferErr  error
	updateWalletErrFor int64
	updateWalletErr    error
	committed          bool
	rolledBack         bool
}

func (m *mockTx) Commit(ctx context.Context) error {
	m.committed = true
	return m.commitErr
}
func (m *mockTx) Rollback(ctx context.Context) error {
	m.rolledBack = true
	return nil
}
func (m *mockTx) GetBalance(ctx context.Context, walletID int64) (int64, error) {
	return m.GetWalletForUpdate(ctx, walletID)
}
func (m *mockTx) GetWalletForUpdate(ctx context.Context, id int64) (int64, error) {
	v, ok := m.wallets[id]
	if !ok {
		return 0, pgx.ErrNoRows
	}
	return v, nil
}
func (m *mockTx) UpdateWallet(ctx context.Context, id int64, balance int64) error {
	if m.updateWalletErr != nil && id == m.updateWalletErrFor {
		return m.updateWalletErr
	}
	m.wallets[id] = balance
	return nil
}
func (m *mockTx) CreateTransfer(ctx context.Context, from, to, amount int64, key string) (int64, error) {
	if m.createTransferErr != nil {
		return 0, m.createTransferErr
	}
	return 123, nil
}
func (m *mockTx) UpdateTransferState(ctx context.Context, transferID int64, state string) error {
	return nil
}
func (m *mockTx) InsertLedgerEntry(ctx context.Context, walletID, transferID int64, typ string, amount int64) error {
	return nil
}

type mockRepo struct {
	claimOK               bool
	claimErr              error
	getResponse           string
	getCode               int
	getErr                error
	beginErr              error
	tx                    repository.Tx
	updatedStatus         string
	updatedCode           int
	updatedResp           string
	updateIdempotencyCall int
}

func (m *mockRepo) ClaimIdempotency(ctx context.Context, key, h string) (bool, error) {
	return m.claimOK, m.claimErr
}
func (m *mockRepo) GetIdempotency(ctx context.Context, key string) (string, int, error) {
	return m.getResponse, m.getCode, m.getErr
}
func (m *mockRepo) UpdateIdempotency(ctx context.Context, key, status, response string, code int) error {
	m.updateIdempotencyCall++
	m.updatedStatus = status
	m.updatedCode = code
	m.updatedResp = response
	return nil
}
func (m *mockRepo) BeginTx(ctx context.Context) (repository.Tx, error) {
	if m.beginErr != nil {
		return nil, m.beginErr
	}
	return m.tx, nil
}

func req() model.TransferRequest {
	return model.TransferRequest{
		IdempotencyKey: "k-1",
		FromWalletID:   1,
		ToWalletID:     2,
		Amount:         100,
	}
}

func TestTransferSuccess(t *testing.T) {
	tx := &mockTx{wallets: map[int64]int64{1: 500, 2: 100}}
	repo := &mockRepo{claimOK: true, tx: tx}
	svc := NewTransferService(repo)

	resp, code, err := svc.Transfer(context.Background(), req())
	if err != nil || code != 200 || resp == nil {
		t.Fatalf("expected success, got code=%d err=%v", code, err)
	}
	if !tx.committed || tx.rolledBack {
		t.Fatalf("expected commit without rollback")
	}
	if tx.wallets[1] != 400 || tx.wallets[2] != 200 {
		t.Fatalf("wallet balances not updated as expected")
	}
	if repo.updatedStatus != "COMPLETED" {
		t.Fatalf("expected idempotency COMPLETED, got %s", repo.updatedStatus)
	}
}

func TestTransferClaimError(t *testing.T) {
	repo := &mockRepo{claimErr: errors.New("db down")}
	svc := NewTransferService(repo)
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected claim error path")
	}
	if repo.updateIdempotencyCall != 0 {
		t.Fatalf("should not update idempotency when claim fails")
	}
}

func TestTransferIdempotencyCacheHit(t *testing.T) {
	repo := &mockRepo{
		claimOK:     false,
		getResponse: `{"TransferID":77,"Status":"PROCESSED"}`,
		getCode:     200,
	}
	svc := NewTransferService(repo)

	resp, code, err := svc.Transfer(context.Background(), req())
	if err != nil || code != 200 {
		t.Fatalf("expected cached response")
	}
	if resp == nil || resp.TransferID != 77 {
		t.Fatalf("expected parsed cached response")
	}
}

func TestTransferIdempotencyCacheHitInvalidJSON(t *testing.T) {
	repo := &mockRepo{
		claimOK:     false,
		getResponse: "{broken",
		getCode:     200,
	}
	svc := NewTransferService(repo)

	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected unmarshal error path")
	}
}

func TestTransferIdempotencyInProgress(t *testing.T) {
	repo := &mockRepo{claimOK: false, getCode: 200}
	svc := NewTransferService(repo)
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 409 {
		t.Fatalf("expected in-progress response")
	}
}

func TestTransferIdempotencyGetError(t *testing.T) {
	repo := &mockRepo{
		claimOK: false,
		getErr:  errors.New("read failed"),
	}
	svc := NewTransferService(repo)
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected idempotency read error path")
	}
}

func TestTransferIdempotencyHashMismatch(t *testing.T) {
	repo := &mockRepo{
		claimErr: repository.ErrIdempotencyHashMismatch,
	}
	svc := NewTransferService(repo)
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 409 {
		t.Fatalf("expected hash mismatch conflict path")
	}
	if err.Error() != "idempotency key hash mismatch" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestTransferValidationErrors(t *testing.T) {
	tx := &mockTx{wallets: map[int64]int64{1: 100}}
	repo := &mockRepo{claimOK: true, tx: tx}
	svc := NewTransferService(repo)

	bad := req()
	bad.Amount = 0
	_, code, err := svc.Transfer(context.Background(), bad)
	if err == nil || code != 400 {
		t.Fatalf("expected invalid amount error")
	}
	if repo.updatedStatus != "FAILED" {
		t.Fatalf("expected failed idempotency status for validation error")
	}

	bad = req()
	bad.ToWalletID = bad.FromWalletID
	_, code, err = svc.Transfer(context.Background(), bad)
	if err == nil || code != 400 {
		t.Fatalf("expected same wallet validation error")
	}
}

func TestTransferBeginAndWalletFailures(t *testing.T) {
	svc := NewTransferService(&mockRepo{claimOK: true, beginErr: errors.New("begin failed")})
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected begin tx error")
	}

	tx := &mockTx{wallets: map[int64]int64{2: 100}}
	svc = NewTransferService(&mockRepo{claimOK: true, tx: tx})
	_, code, err = svc.Transfer(context.Background(), req())
	if err == nil || code != 404 {
		t.Fatalf("expected wallet not found error")
	}
}

func TestTransferBusinessAndMutationFailures(t *testing.T) {
	tx := &mockTx{wallets: map[int64]int64{1: 10, 2: 100}}
	svc := NewTransferService(&mockRepo{claimOK: true, tx: tx})
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 400 || !tx.rolledBack {
		t.Fatalf("expected insufficient balance rollback")
	}

	tx = &mockTx{wallets: map[int64]int64{1: 500, 2: 100}, createTransferErr: errors.New("insert transfer failed")}
	svc = NewTransferService(&mockRepo{claimOK: true, tx: tx})
	_, code, err = svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected create transfer error")
	}

	tx = &mockTx{
		wallets:            map[int64]int64{1: 500, 2: 100},
		updateWalletErrFor: 2,
		updateWalletErr:    errors.New("update failed"),
	}
	svc = NewTransferService(&mockRepo{claimOK: true, tx: tx})
	_, code, err = svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected update wallet error")
	}
}

func TestTransferCommitFailure(t *testing.T) {
	tx := &mockTx{
		wallets:   map[int64]int64{1: 500, 2: 100},
		commitErr: errors.New("commit failed"),
	}
	svc := NewTransferService(&mockRepo{claimOK: true, tx: tx})
	_, code, err := svc.Transfer(context.Background(), req())
	if err == nil || code != 500 {
		t.Fatalf("expected commit error")
	}
	if !tx.committed || !tx.rolledBack {
		t.Fatalf("expected commit attempt and rollback")
	}
}

func TestHashDeterministic(t *testing.T) {
	r := req()
	h1 := hash(r)
	h2 := hash(r)
	if h1 == "" || h1 != h2 {
		t.Fatalf("expected deterministic non-empty hash")
	}
}
