package tests

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/mritunjaysingh22/wallet-transfer-assignment/internal/db"
	"github.com/mritunjaysingh22/wallet-transfer-assignment/internal/service"
)

func TestLedgerEntriesCreated(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 500)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         250,
	}

	resp, err := transferService.CreateTransfer(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	transfer, err := testQueries.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)
	require.NoError(t, err)

	entries, err := testQueries.GetLedgerEntriesByTransfer(
		ctx,
		transfer.ID,
	)

	require.NoError(t, err)
	require.Len(t, entries, 2)

	var debit *db.LedgerEntry
	var credit *db.LedgerEntry

	for i := range entries {

		switch entries[i].EntryType {

		case db.LedgerTypeDEBIT:
			debit = &entries[i]

		case db.LedgerTypeCREDIT:
			credit = &entries[i]
		}
	}

	require.NotNil(t, debit)
	require.NotNil(t, credit)

	assert.Equal(t, int64(250), debit.Amount)
	assert.Equal(t, int64(250), credit.Amount)

	assert.Equal(
		t,
		uuidString(transfer.FromWalletID),
		uuidString(debit.WalletID),
	)

	assert.Equal(
		t,
		uuidString(transfer.ToWalletID),
		uuidString(credit.WalletID),
	)
}

func TestLedgerEntriesAmountMatchTransfer(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 5000)
	to := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         1500,
	}

	_, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)

	transfer, err := testQueries.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)
	require.NoError(t, err)

	entries, err := testQueries.GetLedgerEntriesByTransfer(
		ctx,
		transfer.ID,
	)

	require.NoError(t, err)
	require.Len(t, entries, 2)

	for _, e := range entries {

		assert.Equal(
			t,
			transfer.Amount,
			e.Amount,
		)
	}
}

func TestExactlyOneDebitAndOneCredit(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 1000)
	to := createWallet(t, 1000)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         100,
	}

	_, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)

	transfer, err := testQueries.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)
	require.NoError(t, err)

	entries, err := testQueries.GetLedgerEntriesByTransfer(
		ctx,
		transfer.ID,
	)
	require.NoError(t, err)

	var debitCount int
	var creditCount int

	for _, e := range entries {

		switch e.EntryType {

		case db.LedgerTypeDEBIT:
			debitCount++

		case db.LedgerTypeCREDIT:
			creditCount++
		}
	}

	assert.Equal(t, 1, debitCount)
	assert.Equal(t, 1, creditCount)
}

func TestLedgerEntriesBelongToSameTransfer(t *testing.T) {

	cleanupDatabase()

	from := createWallet(t, 3000)
	to := createWallet(t, 500)

	req := service.TransferRequest{
		IdempotencyKey: uuid.NewString(),
		FromWalletID:   from,
		ToWalletID:     to,
		Amount:         800,
	}

	_, err := transferService.CreateTransfer(ctx, req)
	require.NoError(t, err)

	transfer, err := testQueries.GetTransferByIdempotencyKey(
		ctx,
		req.IdempotencyKey,
	)
	require.NoError(t, err)

	entries, err := testQueries.GetLedgerEntriesByTransfer(
		ctx,
		transfer.ID,
	)
	require.NoError(t, err)

	require.Len(t, entries, 2)

	for _, e := range entries {

		assert.Equal(
			t,
			uuidString(transfer.ID),
			uuidString(e.TransferID),
		)
	}
}
