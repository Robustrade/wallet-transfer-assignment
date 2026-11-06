package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// stubTransfers is a canned TransferUseCase, so the decorator is tested without a database.
type stubTransfers struct {
	result service.CreateTransferResult
	err    error
}

func (s stubTransfers) CreateTransfer(context.Context, domain.TransferRequest) (service.CreateTransferResult, error) {
	return s.result, s.err
}

func (s stubTransfers) GetTransfer(context.Context, uuid.UUID) (domain.Transfer, error) {
	return s.result.Transfer, s.err
}

func newLogged(next service.TransferUseCase) (*service.LoggingTransfers, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return service.NewLoggingTransfers(next, logger), &buf
}

func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var entry map[string]any
	require.NoError(t, json.Unmarshal(lines[len(lines)-1], &entry), buf.String())
	return entry
}

func TestLoggingTransfers_PassesResultsThroughAndLogsTheOutcome(t *testing.T) {
	want := service.CreateTransferResult{
		Transfer: domain.Transfer{ID: uuid.New(), Status: domain.StatusProcessed, Amount: 10},
		Replayed: true,
	}
	logged, buf := newLogged(stubTransfers{result: want})

	got, err := logged.CreateTransfer(context.Background(), request("k", "a", "b", 10))

	require.NoError(t, err)
	require.Equal(t, want, got)
	entry := lastLogLine(t, buf)
	require.Equal(t, "INFO", entry["level"])
	require.Equal(t, "transfer completed", entry["msg"])
	require.Equal(t, want.Transfer.ID.String(), entry["transfer_id"])
	require.Equal(t, true, entry["replayed"])
}

func TestLoggingTransfers_LogsFailuresAtALevelMatchingWhoMustAct(t *testing.T) {
	tests := []struct {
		err       error
		wantLevel string
		wantMsg   string
	}{
		{&domain.ValidationError{Field: "amount", Message: "bad"}, "INFO", "transfer rejected"},
		{fmt.Errorf("x: %w", domain.ErrWalletNotFound), "INFO", "transfer rejected"},
		{domain.ErrIdempotencyKeyReused, "INFO", "transfer rejected"},
		{fmt.Errorf("x: %w", domain.ErrBusy), "WARN", "transfer busy, client may retry"},
		{errors.New("connection reset"), "ERROR", "transfer failed unexpectedly"},
	}
	for _, tc := range tests {
		t.Run(tc.wantLevel+" "+tc.err.Error(), func(t *testing.T) {
			logged, buf := newLogged(stubTransfers{err: tc.err})

			_, err := logged.CreateTransfer(context.Background(), request("k", "a", "b", 10))

			require.ErrorIs(t, err, tc.err, "the error is returned unchanged")
			entry := lastLogLine(t, buf)
			require.Equal(t, tc.wantLevel, entry["level"])
			require.Equal(t, tc.wantMsg, entry["msg"])
			require.Equal(t, "k", entry["idempotency_key"])
		})
	}
}

func TestLoggingTransfers_GetTransferIsAPassThrough(t *testing.T) {
	want := domain.Transfer{ID: uuid.New()}
	logged, buf := newLogged(stubTransfers{result: service.CreateTransferResult{Transfer: want}})

	got, err := logged.GetTransfer(context.Background(), want.ID)

	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Empty(t, buf.String(), "reads are covered by the access log")
}
