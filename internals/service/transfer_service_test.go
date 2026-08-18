package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// --- pure helper functions ---

func TestOrderedIDs_Deterministic(t *testing.T) {
	a := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	b := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	first, second := orderedIDs(a, b)
	require.Equal(t, a, first)
	require.Equal(t, b, second)

	// Calling with arguments reversed must produce the same order — this is
	// what prevents deadlocks between two transfers moving money in opposite
	// directions between the same wallet pair.
	first, second = orderedIDs(b, a)
	require.Equal(t, a, first)
	require.Equal(t, b, second)
}

func TestHashRequest_Deterministic(t *testing.T) {
	in := CreateTransferInput{
		FromWalletID: uuid.New(),
		ToWalletID:   uuid.New(),
		Amount:       100,
	}
	h1 := hashRequest(in)
	h2 := hashRequest(in)
	require.Equal(t, h1, h2)

	other := in
	other.Amount = 200
	require.NotEqual(t, h1, hashRequest(other))
}

func TestIsUniqueViolation(t *testing.T) {
	require.False(t, isUniqueViolation(errors.New("boom")))
	require.False(t, isUniqueViolation(&pgconn.PgError{Code: "23503"})) // foreign_key_violation
	require.True(t, isUniqueViolation(&pgconn.PgError{Code: "23505"}))
	require.True(t, isUniqueViolation(errWrap(&pgconn.PgError{Code: "23505"})), "must unwrap")
}

func errWrap(err error) error {
	return errors.Join(err)
}

func TestToResult(t *testing.T) {
	tr := &models.Transfer{
		ID:            uuid.New(),
		FromWalletID:  uuid.New(),
		ToWalletID:    uuid.New(),
		Amount:        50,
		State:         models.TransferFailed,
		FailureReason: "insufficient balance",
	}
	res := toResult(tr)
	require.Equal(t, tr.ID, res.TransferID)
	require.Equal(t, tr.State, res.State)
	require.Equal(t, tr.FromWalletID, res.FromWalletID)
	require.Equal(t, tr.ToWalletID, res.ToWalletID)
	require.Equal(t, tr.Amount, res.Amount)
	require.Equal(t, tr.FailureReason, res.FailureReason)
}

// --- CreateTransfer orchestration, with mocked repositories ---

func newTestService(walletRepo *fakeWalletRepo, transferRepo *fakeTransferRepo, ledgerRepo *fakeLedgerRepo, idempRepo *fakeIdempRepo) *TransferService {
	return NewTransferService(fakeTxManager{}, walletRepo, transferRepo, ledgerRepo, idempRepo)
}

func TestCreateTransfer_HappyPath(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	walletRepo := &fakeWalletRepo{wallets: map[uuid.UUID]*models.Wallet{
		from: {ID: from, Balance: 1000},
		to:   {ID: to, Balance: 500},
	}}
	transferRepo := &fakeTransferRepo{}
	ledgerRepo := &fakeLedgerRepo{}
	idempRepo := &fakeIdempRepo{}

	svc := newTestService(walletRepo, transferRepo, ledgerRepo, idempRepo)

	result, err := svc.CreateTransfer(t.Context(), CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	require.NoError(t, err)
	require.Equal(t, models.TransferProcessed, result.State)

	require.Len(t, walletRepo.updated, 2)
	require.EqualValues(t, 900, walletRepo.updated[0].Balance, "source wallet debited")
	require.EqualValues(t, 600, walletRepo.updated[1].Balance, "destination wallet credited")

	require.Len(t, ledgerRepo.inserted, 2)
	require.Equal(t, models.LedgerDebit, ledgerRepo.inserted[0].Type)
	require.Equal(t, from, ledgerRepo.inserted[0].WalletID)
	require.Equal(t, models.LedgerCredit, ledgerRepo.inserted[1].Type)
	require.Equal(t, to, ledgerRepo.inserted[1].WalletID)

	require.Len(t, transferRepo.updatedStates, 1)
	require.Equal(t, models.TransferProcessed, transferRepo.updatedStates[0].State)

	require.Len(t, idempRepo.saved, 1)
	require.Equal(t, 201, idempRepo.saved[0].StatusCode)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	walletRepo := &fakeWalletRepo{wallets: map[uuid.UUID]*models.Wallet{
		from: {ID: from, Balance: 10},
		to:   {ID: to, Balance: 500},
	}}
	transferRepo := &fakeTransferRepo{}
	ledgerRepo := &fakeLedgerRepo{}
	idempRepo := &fakeIdempRepo{}

	svc := newTestService(walletRepo, transferRepo, ledgerRepo, idempRepo)

	result, err := svc.CreateTransfer(t.Context(), CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	require.NoError(t, err, "insufficient funds is a terminal business outcome, not a transport error")
	require.Equal(t, models.TransferFailed, result.State)
	require.Equal(t, models.ErrInsufficientBalance.Error(), result.FailureReason)

	require.Empty(t, walletRepo.updated, "no balance should be persisted on a failed transfer")
	require.Empty(t, ledgerRepo.inserted, "no ledger entries should be written on a failed transfer")

	require.Len(t, transferRepo.updatedStates, 1)
	require.Equal(t, models.TransferFailed, transferRepo.updatedStates[0].State)

	require.Len(t, idempRepo.saved, 1)
	require.Equal(t, 422, idempRepo.saved[0].StatusCode, "failed transfers are cached as 422 for idempotent replay")
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	walletRepo := &fakeWalletRepo{wallets: map[uuid.UUID]*models.Wallet{
		to: {ID: to, Balance: 500},
		// from wallet deliberately missing
	}}
	transferRepo := &fakeTransferRepo{}
	ledgerRepo := &fakeLedgerRepo{}
	idempRepo := &fakeIdempRepo{}

	svc := newTestService(walletRepo, transferRepo, ledgerRepo, idempRepo)

	result, err := svc.CreateTransfer(t.Context(), CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, repository.ErrWalletNotFound)
	require.Empty(t, transferRepo.created, "no transfer row should be created when a wallet doesn't exist")
}

func TestCreateTransfer_IdempotentReplay_SamePayload(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	in := CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	cached := TransferResult{
		TransferID:   uuid.New(),
		State:        models.TransferProcessed,
		FromWalletID: from,
		ToWalletID:   to,
		Amount:       100,
	}
	body, err := json.Marshal(cached)
	require.NoError(t, err)

	walletRepo := &fakeWalletRepo{wallets: map[uuid.UUID]*models.Wallet{}}
	transferRepo := &fakeTransferRepo{}
	ledgerRepo := &fakeLedgerRepo{}
	idempRepo := &fakeIdempRepo{
		getRec: &repository.IdempotencyRecord{
			IdempotencyKey: "key-1",
			RequestHash:    hashRequest(in),
			ResponseBody:   body,
			StatusCode:     201,
		},
	}

	svc := newTestService(walletRepo, transferRepo, ledgerRepo, idempRepo)

	result, err := svc.CreateTransfer(t.Context(), in)
	require.NoError(t, err)
	require.Equal(t, cached.TransferID, result.TransferID)

	require.Empty(t, transferRepo.created, "a replayed request must not create a new transfer")
	require.Empty(t, ledgerRepo.inserted, "a replayed request must not write new ledger entries")
}

func TestCreateTransfer_IdempotencyConflict_DifferentPayload(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	idempRepo := &fakeIdempRepo{
		getRec: &repository.IdempotencyRecord{
			IdempotencyKey: "key-1",
			RequestHash:    "some-other-hash",
			ResponseBody:   []byte(`{}`),
			StatusCode:     201,
		},
	}
	svc := newTestService(&fakeWalletRepo{}, &fakeTransferRepo{}, &fakeLedgerRepo{}, idempRepo)

	result, err := svc.CreateTransfer(t.Context(), CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrIdempotencyKeyConflict)
}

// TestCreateTransfer_UniqueViolationRace covers the case lookupIdempotencyRecord
// misses because a concurrent request with the same key is mid-flight: this
// request's own Create() then loses the DB-level unique-constraint race, and
// the service must resolve that by returning whatever the winner produced.
func TestCreateTransfer_UniqueViolationRace(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	winner := &models.Transfer{
		ID:           uuid.New(),
		FromWalletID: from,
		ToWalletID:   to,
		Amount:       100,
		State:        models.TransferProcessed,
	}

	walletRepo := &fakeWalletRepo{wallets: map[uuid.UUID]*models.Wallet{
		from: {ID: from, Balance: 1000},
		to:   {ID: to, Balance: 500},
	}}
	transferRepo := &fakeTransferRepo{
		createErr: &pgconn.PgError{Code: "23505"},
		byKey:     map[string]*models.Transfer{"key-1": winner},
	}
	ledgerRepo := &fakeLedgerRepo{}
	idempRepo := &fakeIdempRepo{}

	svc := newTestService(walletRepo, transferRepo, ledgerRepo, idempRepo)

	result, err := svc.CreateTransfer(t.Context(), CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	require.NoError(t, err)
	require.Equal(t, winner.ID, result.TransferID)
	require.Empty(t, walletRepo.updated, "the losing request must not also move money")
	require.Empty(t, ledgerRepo.inserted)
}

func TestCreateTransfer_UniqueViolationRace_WinnerRowMissing(t *testing.T) {
	from, to := uuid.New(), uuid.New()
	walletRepo := &fakeWalletRepo{wallets: map[uuid.UUID]*models.Wallet{
		from: {ID: from, Balance: 1000},
		to:   {ID: to, Balance: 500},
	}}
	transferRepo := &fakeTransferRepo{
		createErr: &pgconn.PgError{Code: "23505"},
		// byKey deliberately empty: simulates the unexpected state where the
		// unique-constraint race fired but the winning row can't be found.
	}
	svc := newTestService(walletRepo, transferRepo, &fakeLedgerRepo{}, &fakeIdempRepo{})

	result, err := svc.CreateTransfer(t.Context(), CreateTransferInput{
		IdempotencyKey: "key-1",
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	})

	require.Nil(t, result)
	require.Error(t, err)
}
