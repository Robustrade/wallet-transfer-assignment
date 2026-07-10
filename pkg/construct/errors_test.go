package construct_test

import (
	"context"
	"testing"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/domain"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/construct"
	"github.com/stretchr/testify/assert"
)

func TestMapTransferError(t *testing.T) {
	err := construct.MapTransferError(domain.ErrInsufficientBalance)

	assert.Equal(t, construct.ErrInsufficientBalance, err)

	err = construct.MapTransferError(domain.ErrWalletNotFound)
	assert.Equal(t, construct.ErrWalletNotFound, err)

	err = construct.MapTransferError(domain.ErrSameWallet)
	assert.Equal(t, construct.ErrSameWallet, err)

	err = construct.MapTransferError(domain.ErrIdempotencyKeyConflict)
	assert.Equal(t, construct.ErrIdempotencyKeyConflict, err)

	err = construct.MapTransferError(domain.ErrTransferFailed)
	assert.Equal(t, construct.ErrTransferFailed, err)

	err = construct.MapTransferError(context.DeadlineExceeded)
	assert.Equal(t, construct.ErrTransferTimeout, err)

	err = construct.MapTransferError(nil)
	assert.Equal(t, construct.ErrTransferFailed, err)

}
