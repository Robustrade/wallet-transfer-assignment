package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTransfer_Validation(t *testing.T) {
	now := time.Now()

	_, err := NewTransfer("t1", "", "w1", "w2", 100, "hash", now)
	assert.ErrorIs(t, err, ErrIdempotencyKeyRequired)

	_, err = NewTransfer("t1", "key", "w1", "w2", 0, "hash", now)
	assert.ErrorIs(t, err, ErrInvalidAmount)

	_, err = NewTransfer("t1", "key", "w1", "w2", -5, "hash", now)
	assert.ErrorIs(t, err, ErrInvalidAmount)

	_, err = NewTransfer("t1", "key", "w1", "w1", 100, "hash", now)
	assert.ErrorIs(t, err, ErrSameWallet)

	tr, err := NewTransfer("t1", "key", "w1", "w2", 100, "hash", now)
	require.NoError(t, err)
	assert.Equal(t, TransferPending, tr.Status)
}

func TestTransfer_StateMachine(t *testing.T) {
	now := time.Now()

	t.Run("pending to processed", func(t *testing.T) {
		tr, err := NewTransfer("t1", "key", "w1", "w2", 100, "hash", now)
		require.NoError(t, err)
		require.NoError(t, tr.MarkProcessed(now))
		assert.Equal(t, TransferProcessed, tr.Status)
	})

	t.Run("pending to failed", func(t *testing.T) {
		tr, err := NewTransfer("t1", "key", "w1", "w2", 100, "hash", now)
		require.NoError(t, err)
		require.NoError(t, tr.MarkFailed("insufficient funds", now))
		assert.Equal(t, TransferFailed, tr.Status)
		assert.Equal(t, "insufficient funds", tr.FailureReason)
	})

	t.Run("processed is terminal", func(t *testing.T) {
		tr, err := NewTransfer("t1", "key", "w1", "w2", 100, "hash", now)
		require.NoError(t, err)
		require.NoError(t, tr.MarkProcessed(now))
		assert.ErrorIs(t, tr.MarkProcessed(now), ErrInvalidStateTransition)
		assert.ErrorIs(t, tr.MarkFailed("x", now), ErrInvalidStateTransition)
	})

	t.Run("failed is terminal", func(t *testing.T) {
		tr, err := NewTransfer("t1", "key", "w1", "w2", 100, "hash", now)
		require.NoError(t, err)
		require.NoError(t, tr.MarkFailed("x", now))
		assert.ErrorIs(t, tr.MarkFailed("y", now), ErrInvalidStateTransition)
		assert.ErrorIs(t, tr.MarkProcessed(now), ErrInvalidStateTransition)
	})
}

func TestWallet_CanDebit(t *testing.T) {
	w := &Wallet{Balance: 100}
	assert.True(t, w.CanDebit(100))
	assert.True(t, w.CanDebit(50))
	assert.False(t, w.CanDebit(101))
}
