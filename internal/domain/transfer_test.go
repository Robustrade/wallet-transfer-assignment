package domain_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/celinagupta/wallet-transfer-assignment/internal/domain"
)

func validRequest() domain.TransferRequest {
	return domain.TransferRequest{
		IdempotencyKey: "abc123",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
}

func TestTransferRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(r *domain.TransferRequest)
		wantField string // empty means the request is valid
	}{
		{name: "valid request", mutate: func(*domain.TransferRequest) {}},
		{name: "no idempotency key is allowed", mutate: func(r *domain.TransferRequest) { r.IdempotencyKey = "" }},
		{name: "key at max length", mutate: func(r *domain.TransferRequest) {
			r.IdempotencyKey = strings.Repeat("k", domain.MaxIdempotencyKeyLength)
		}},
		{name: "key too long", wantField: "idempotencyKey", mutate: func(r *domain.TransferRequest) {
			r.IdempotencyKey = strings.Repeat("k", domain.MaxIdempotencyKeyLength+1)
		}},
		{name: "key length counts characters, not bytes", mutate: func(r *domain.TransferRequest) {
			r.IdempotencyKey = strings.Repeat("é", domain.MaxIdempotencyKeyLength) // 2 bytes each
		}},
		{name: "missing source wallet", wantField: "fromWalletId", mutate: func(r *domain.TransferRequest) { r.FromWalletID = "" }},
		{name: "missing destination wallet", wantField: "toWalletId", mutate: func(r *domain.TransferRequest) { r.ToWalletID = "" }},
		{name: "same source and destination", wantField: "toWalletId", mutate: func(r *domain.TransferRequest) { r.ToWalletID = r.FromWalletID }},
		// PostgreSQL TEXT cannot store NUL; reject it here instead of failing in the database.
		{name: "NUL in idempotency key", wantField: "idempotencyKey", mutate: func(r *domain.TransferRequest) { r.IdempotencyKey = "a\x00b" }},
		{name: "NUL in source wallet", wantField: "fromWalletId", mutate: func(r *domain.TransferRequest) { r.FromWalletID = "wallet\x001" }},
		{name: "NUL in destination wallet", wantField: "toWalletId", mutate: func(r *domain.TransferRequest) { r.ToWalletID = "wallet\x002" }},
		{name: "zero amount", wantField: "amount", mutate: func(r *domain.TransferRequest) { r.Amount = 0 }},
		{name: "negative amount", wantField: "amount", mutate: func(r *domain.TransferRequest) { r.Amount = -5 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := validRequest()
			tc.mutate(&req)

			err := req.Validate()

			if tc.wantField == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrValidation)
			var vErr *domain.ValidationError
			require.True(t, errors.As(err, &vErr), "want *ValidationError, got %T", err)
			assert.Equal(t, tc.wantField, vErr.Field)
		})
	}
}

func TestNewTransferStartsPending(t *testing.T) {
	id := uuid.New()
	req := validRequest()

	tr := domain.NewTransfer(id, req)

	assert.Equal(t, id, tr.ID)
	assert.Equal(t, domain.StatusPending, tr.Status)
	assert.Empty(t, tr.FailureReason)
	assert.Equal(t, req.IdempotencyKey, tr.IdempotencyKey)
	assert.Equal(t, req.FromWalletID, tr.FromWalletID)
	assert.Equal(t, req.ToWalletID, tr.ToWalletID)
	assert.Equal(t, req.Amount, tr.Amount)
}

func TestTransferStatusTransitions(t *testing.T) {
	t.Run("pending to processed", func(t *testing.T) {
		tr := domain.NewTransfer(uuid.New(), validRequest())

		require.NoError(t, tr.MarkProcessed())
		assert.Equal(t, domain.StatusProcessed, tr.Status)
	})

	t.Run("pending to failed records the reason", func(t *testing.T) {
		tr := domain.NewTransfer(uuid.New(), validRequest())

		require.NoError(t, tr.MarkFailed(domain.FailureInsufficientFunds))
		assert.Equal(t, domain.StatusFailed, tr.Status)
		assert.Equal(t, domain.FailureInsufficientFunds, tr.FailureReason)
	})

	t.Run("failing requires a reason", func(t *testing.T) {
		tr := domain.NewTransfer(uuid.New(), validRequest())

		require.Error(t, tr.MarkFailed(""))
		assert.Equal(t, domain.StatusPending, tr.Status)
	})

	terminal := map[string]func(*domain.Transfer) error{
		"processed": (*domain.Transfer).MarkProcessed,
		"failed":    func(tr *domain.Transfer) error { return tr.MarkFailed(domain.FailureInsufficientFunds) },
	}
	next := map[string]func(*domain.Transfer) error{
		"to processed": (*domain.Transfer).MarkProcessed,
		"to failed":    func(tr *domain.Transfer) error { return tr.MarkFailed(domain.FailureInsufficientFunds) },
	}
	for fromName, reach := range terminal {
		for toName, move := range next {
			t.Run(fromName+" is terminal: cannot move "+toName, func(t *testing.T) {
				tr := domain.NewTransfer(uuid.New(), validRequest())
				require.NoError(t, reach(&tr))
				before := tr

				err := move(&tr)

				assert.ErrorIs(t, err, domain.ErrInvalidTransition)
				assert.Equal(t, before, tr, "a rejected transition must not change the transfer")
			})
		}
	}
}

func TestMatchesRequest(t *testing.T) {
	req := validRequest()
	tr := domain.NewTransfer(uuid.New(), req)

	assert.True(t, tr.MatchesRequest(req), "identical payload is a retry")
}

// TestMatchesRequestDetectsEveryPayloadField changes each payload field of
// TransferRequest in turn and expects a mismatch. If a field is added to the
// request but not compared in MatchesRequest, this test fails, so a reused key
// with a changed payload can never be replayed by mistake.
func TestMatchesRequestDetectsEveryPayloadField(t *testing.T) {
	base := validRequest()
	tr := domain.NewTransfer(uuid.New(), base)

	typ := reflect.TypeOf(base)
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Name == "IdempotencyKey" {
			continue // the key is how the stored transfer is found, not part of the payload
		}
		t.Run(field.Name, func(t *testing.T) {
			changed := base
			v := reflect.ValueOf(&changed).Elem().Field(i)
			switch v.Kind() {
			case reflect.String:
				v.SetString(v.String() + "_changed")
			case reflect.Int64:
				v.SetInt(v.Int() + 1)
			default:
				t.Fatalf("field %s has kind %s: extend this test to change it", field.Name, v.Kind())
			}

			assert.False(t, tr.MatchesRequest(changed), "changing %s must not match", field.Name)
		})
	}
}

func TestLedgerEntriesAreBalancedPair(t *testing.T) {
	tr := domain.NewTransfer(uuid.New(), validRequest())
	require.NoError(t, tr.MarkProcessed())

	entries := tr.LedgerEntries()

	require.Len(t, entries, 2)
	assert.Equal(t, domain.LedgerEntry{
		TransferID: tr.ID, WalletID: tr.FromWalletID, Type: domain.EntryDebit, Amount: tr.Amount,
	}, entries[0])
	assert.Equal(t, domain.LedgerEntry{
		TransferID: tr.ID, WalletID: tr.ToWalletID, Type: domain.EntryCredit, Amount: tr.Amount,
	}, entries[1])
}

func TestLedgerEntriesOnlyForProcessedTransfers(t *testing.T) {
	pending := domain.NewTransfer(uuid.New(), validRequest())
	failed := domain.NewTransfer(uuid.New(), validRequest())
	require.NoError(t, failed.MarkFailed(domain.FailureInsufficientFunds))

	assert.Empty(t, pending.LedgerEntries(), "pending transfer moves no money")
	assert.Empty(t, failed.LedgerEntries(), "failed transfer moves no money")
}
