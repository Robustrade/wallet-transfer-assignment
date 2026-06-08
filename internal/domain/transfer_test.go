package domain

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransfer_Transition(t *testing.T) {
	newTransfer := func() *Transfer {
		return &Transfer{ID: uuid.New(), Status: StatusPending}
	}

	t.Run("pending to processed", func(t *testing.T) {
		tr := newTransfer()
		require.NoError(t, tr.Transition(StatusProcessed))
		assert.Equal(t, StatusProcessed, tr.Status)
	})

	t.Run("pending to failed", func(t *testing.T) {
		tr := newTransfer()
		require.NoError(t, tr.Transition(StatusFailed))
		assert.Equal(t, StatusFailed, tr.Status)
	})

	t.Run("processed cannot transition", func(t *testing.T) {
		tr := newTransfer()
		_ = tr.Transition(StatusProcessed)
		assert.Error(t, tr.Transition(StatusFailed))
	})

	t.Run("failed cannot transition", func(t *testing.T) {
		tr := newTransfer()
		_ = tr.Transition(StatusFailed)
		assert.Error(t, tr.Transition(StatusProcessed))
	})

	t.Run("pending to pending is invalid", func(t *testing.T) {
		tr := newTransfer()
		assert.Error(t, tr.Transition(StatusPending))
	})
}
