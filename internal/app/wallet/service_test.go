package wallet

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/domain"
	"github.com/amantiwari/wallet-transfer/internal/testutil/memstore"
)

func TestCreateAndGetWallet(t *testing.T) {
	store := memstore.New()
	svc := NewService(store, store, clockAt(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), slog.New(slog.DiscardHandler))

	created, err := svc.Create(context.Background(), CreateCommand{ID: "wallet_1", InitialBalance: 25})
	if err != nil {
		t.Fatal(err)
	}
	if created.Balance != 25 || created.InitialBalance != 25 {
		t.Fatalf("created = %+v", created)
	}
	got, err := svc.Get(context.Background(), "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != created.ID || got.Balance != created.Balance {
		t.Fatalf("got = %+v", got)
	}

	_, err = svc.Create(context.Background(), CreateCommand{ID: "wallet_1", InitialBalance: 1})
	if !errors.Is(err, domain.ErrWalletExists) {
		t.Fatalf("duplicate = %v", err)
	}
	_, err = svc.Get(context.Background(), "missing")
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("missing = %v", err)
	}
	_, err = svc.Create(context.Background(), CreateCommand{ID: "bad id", InitialBalance: 1})
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("validation = %v", err)
	}
}

type clockAt time.Time

func (c clockAt) Now() time.Time { return time.Time(c) }
