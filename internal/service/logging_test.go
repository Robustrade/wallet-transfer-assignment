package service

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"wallet-transfer-assignment/internal/repository"
)

func TestTransferStructuredEvents(t *testing.T) {
	for _, tc := range []struct {
		name, fail      string
		amount          int64
		retry, conflict bool
		event, status   string
	}{
		{name: "success", amount: 30, event: "transfer_committed", status: "PROCESSED"},
		{name: "insufficient", amount: 101, event: "transfer_committed", status: "FAILED"},
		{name: "duplicate", amount: 30, retry: true, event: "duplicate_idempotent_request", status: "PROCESSED"},
		{name: "failed duplicate", amount: 101, retry: true, event: "duplicate_idempotent_request", status: "FAILED"},
		{name: "conflict", amount: 30, conflict: true, event: "idempotency_conflict", status: "UNKNOWN"},
		{name: "rollback", amount: 30, fail: "ledger", event: "transaction_failed", status: "UNKNOWN"},
		{name: "commit failure", amount: 30, fail: "commit", event: "transaction_failed", status: "UNKNOWN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			m := fixture()
			s := New(m, slog.New(slog.NewJSONHandler(&output, nil)))
			req := request()
			req.Amount = tc.amount
			if tc.retry || tc.conflict {
				if _, err := s.Transfer(context.Background(), req); err != nil {
					t.Fatal(err)
				}
				output.Reset()
			}
			if tc.conflict {
				req.Amount++
			}
			m.fail = tc.fail
			_, _ = s.Transfer(context.Background(), req)
			events := map[string]map[string]any{}
			for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(err)
				}
				if row["idempotency_key"] != "key" || row["from_wallet_id"] != "a" || row["to_wallet_id"] != "b" || row["transfer_id"] == "" {
					t.Fatalf("missing context: %v", row)
				}
				event, _ := row["event"].(string)
				events[event] = row
			}
			if events["request_accepted"] == nil || events[tc.event] == nil || events[tc.event]["status"] != tc.status {
				t.Fatalf("wrong events: %s", output.String())
			}
			if tc.fail != "" && (events["transfer_committed"] != nil || events["duplicate_idempotent_request"] != nil) {
				t.Fatal("failed transaction logged success")
			}
			if tc.amount == 101 && !tc.retry && events["insufficient_balance"] == nil {
				t.Fatal("missing insufficient balance event")
			}
			if strings.Contains(output.String(), injected.Error()) {
				t.Fatal("raw error leaked")
			}
			if tc.retry {
				for _, transfer := range m.transfers {
					if events[tc.event]["transfer_id"] != transfer.ID {
						t.Fatal("duplicate logged a new transfer ID")
					}
				}
			}
		})
	}
}

type panickingWork struct{}

func (panickingWork) WithinTransaction(context.Context, func(context.Context, repository.Tx) error) error {
	panic("private failure details")
}
func TestPanicDoesNotLogCommittedTransfer(t *testing.T) {
	var output bytes.Buffer
	s := New(panickingWork{}, slog.New(slog.NewJSONHandler(&output, nil)))
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic must propagate")
			}
		}()
		_, _ = s.Transfer(context.Background(), request())
	}()
	if strings.Contains(output.String(), "transfer_committed") || strings.Contains(output.String(), "private failure details") {
		t.Fatalf("misleading or sensitive panic log: %s", output.String())
	}
	if !strings.Contains(output.String(), "transaction_failed") {
		t.Fatal("panic failure not logged")
	}
}
