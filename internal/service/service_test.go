package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"testing"

	"wallet-transfer-assignment/internal/domain"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

func TestCreateTransferMovesBalanceAndWritesBothEntries(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	mustCreateWallet(t, svc, "wallet_1", 500)
	mustCreateWallet(t, svc, "wallet_2", 20)

	result := mustTransfer(t, svc, Request{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if result.Status != http.StatusCreated || result.Replayed {
		t.Fatalf("status=%d replayed=%v", result.Status, result.Replayed)
	}
	view := decodeTransfer(t, result.Body)
	if view.Status != string(domain.StatusProcessed) || view.Amount != 100 {
		t.Fatalf("view = %+v", view)
	}

	assertBalance(t, store, "wallet_1", 400)
	assertBalance(t, store, "wallet_2", 120)
	_, entries, err := store.GetTransfer(ctx, view.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d", len(entries))
	}
	var debit, credit int64
	for _, entry := range entries {
		if entry.Amount != 100 {
			t.Fatalf("entry amount = %d", entry.Amount)
		}
		switch entry.Type {
		case domain.Debit:
			debit += entry.Amount
			if entry.WalletID != "wallet_1" {
				t.Fatalf("debit wallet = %s", entry.WalletID)
			}
		case domain.Credit:
			credit += entry.Amount
			if entry.WalletID != "wallet_2" {
				t.Fatalf("credit wallet = %s", entry.WalletID)
			}
		default:
			t.Fatalf("type = %s", entry.Type)
		}
	}
	if debit != credit {
		t.Fatalf("debit %d credit %d", debit, credit)
	}
}

func TestCreateTransferReplayDoesNotMoveMoneyAgain(t *testing.T) {
	svc, store := newTestService(t)
	mustCreateWallet(t, svc, "wallet_1", 500)
	mustCreateWallet(t, svc, "wallet_2", 20)
	req := Request{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	first := mustTransfer(t, svc, req)
	second := mustTransfer(t, svc, req)
	if !second.Replayed {
		t.Fatal("second call was not a replay")
	}
	if second.Status != first.Status || !bytes.Equal(second.Body, first.Body) {
		t.Fatalf("replay changed the result: %s vs %s", second.Body, first.Body)
	}
	assertBalance(t, store, "wallet_1", 400)
	assertBalance(t, store, "wallet_2", 120)
}

func TestCreateTransferRejectsDifferentPayloadForSameKey(t *testing.T) {
	svc, store := newTestService(t)
	mustCreateWallet(t, svc, "wallet_1", 500)
	mustCreateWallet(t, svc, "wallet_2", 20)
	mustTransfer(t, svc, Request{IdempotencyKey: "abc123", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100})

	_, err := svc.CreateTransfer(context.Background(), Request{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50,
	})
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("err = %v", err)
	}
	assertBalance(t, store, "wallet_1", 400)
	assertBalance(t, store, "wallet_2", 120)
}

func TestInsufficientFundsStoresFailedTransferWithoutLedger(t *testing.T) {
	svc, store := newTestService(t)
	ctx := context.Background()
	mustCreateWallet(t, svc, "wallet_1", 40)
	mustCreateWallet(t, svc, "wallet_2", 10)
	req := Request{IdempotencyKey: "short", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}

	first := mustTransfer(t, svc, req)
	if first.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body = %s", first.Status, first.Body)
	}
	view := decodeTransfer(t, first.Body)
	if view.Status != string(domain.StatusFailed) || view.FailureReason != domain.FailureInsufficientFunds {
		t.Fatalf("view = %+v", view)
	}
	_, entries, err := store.GetTransfer(ctx, view.TransferID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed transfer wrote %d ledger entries", len(entries))
	}

	second := mustTransfer(t, svc, req)
	if !second.Replayed || !bytes.Equal(first.Body, second.Body) {
		t.Fatal("failed result was not replayed")
	}
	assertBalance(t, store, "wallet_1", 40)
	assertBalance(t, store, "wallet_2", 10)
}

func TestMissingWalletDoesNotConsumeIdempotencyKey(t *testing.T) {
	svc, store := newTestService(t)
	req := Request{IdempotencyKey: "later", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 15}
	_, err := svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}

	mustCreateWallet(t, svc, "wallet_1", 100)
	mustCreateWallet(t, svc, "wallet_2", 0)
	result := mustTransfer(t, svc, req)
	if result.Status != http.StatusCreated || result.Replayed {
		t.Fatalf("retry after create: status=%d replayed=%v", result.Status, result.Replayed)
	}
	assertBalance(t, store, "wallet_1", 85)
	assertBalance(t, store, "wallet_2", 15)
}

func TestCreditOverflowFailsWithoutChangingBalances(t *testing.T) {
	svc, store := newTestService(t)
	mustCreateWallet(t, svc, "wallet_1", 5)
	mustCreateWallet(t, svc, "wallet_2", math.MaxInt64)
	result := mustTransfer(t, svc, Request{
		IdempotencyKey: "overflow",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1,
	})
	if result.Status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", result.Status)
	}
	view := decodeTransfer(t, result.Body)
	if view.FailureReason != domain.FailureBalanceOverflow {
		t.Fatalf("reason = %s", view.FailureReason)
	}
	assertBalance(t, store, "wallet_1", 5)
	assertBalance(t, store, "wallet_2", math.MaxInt64)
}

func TestCreateWalletRejectsDuplicateAndNegativeBalance(t *testing.T) {
	svc, _ := newTestService(t)
	mustCreateWallet(t, svc, "wallet_1", 0)
	_, err := svc.CreateWallet(context.Background(), "wallet_1", 10)
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("duplicate err = %v", err)
	}
	_, err = svc.CreateWallet(context.Background(), "wallet_2", -1)
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("negative err = %v", err)
	}
}

func newTestService(t *testing.T) (*Service, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	return New(store), store
}

func mustCreateWallet(t *testing.T, svc *Service, id string, balance int64) {
	t.Helper()
	if _, err := svc.CreateWallet(context.Background(), id, balance); err != nil {
		t.Fatal(err)
	}
}

func mustTransfer(t *testing.T, svc *Service, req Request) Result {
	t.Helper()
	result, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeTransfer(t *testing.T, body []byte) TransferView {
	t.Helper()
	var view TransferView
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func assertBalance(t *testing.T, store *fakeStore, id string, want int64) {
	t.Helper()
	wallet, err := store.GetWallet(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != want {
		t.Fatalf("%s balance = %d, want %d", id, wallet.Balance, want)
	}
}
