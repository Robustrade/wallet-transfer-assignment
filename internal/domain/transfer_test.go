package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/domain"
)

func TestTransferRequest_Validate(t *testing.T) {
	t.Parallel()

	valid := domain.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
	require.NoError(t, valid.Validate())

	sameWallet := valid
	sameWallet.ToWalletID = "wallet_1"
	require.ErrorIs(t, sameWallet.Validate(), domain.ErrValidation)

	zeroAmount := valid
	zeroAmount.Amount = 0
	require.ErrorIs(t, zeroAmount.Validate(), domain.ErrValidation)

	emptyKey := valid
	emptyKey.IdempotencyKey = ""
	require.ErrorIs(t, emptyKey.Validate(), domain.ErrValidation)
}

func TestTransfer_StateTransitions(t *testing.T) {
	t.Parallel()

	req := domain.TransferRequest{
		IdempotencyKey: "key-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	transfer, err := domain.NewPendingTransfer(uuid.New(), req, "INR")
	require.NoError(t, err)
	require.Equal(t, domain.StatusPending, transfer.Status)

	require.NoError(t, transfer.MarkProcessed())
	require.Equal(t, domain.StatusProcessed, transfer.Status)

	require.ErrorIs(t, transfer.MarkProcessed(), domain.ErrInvalidStateTransition)
}
