package apitest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestRequestLogLine(t *testing.T) {
	t.Parallel()

	t.Run("PROCESSED transfer", func(t *testing.T) {
		beginTest(t)
		from := createWallet(t, 1000)
		to := createWallet(t, 0)
		key := newKey()
		var logs syncBuffer
		server := startServer(t, databaseURL, slog.New(slog.NewJSONHandler(&logs, nil)))

		got := postTransferTo(t, server, transferBody(key, from, to, 100)).transfer(t, http.StatusCreated)

		want := map[string]any{
			"level":          "INFO",
			"msg":            "transfer request",
			"idempotencyKey": key,
			"transferId":     got.TransferID,
			"state":          "PROCESSED",
			"status":         float64(http.StatusCreated),
		}
		if record := onlyLogRecord(t, &logs); !reflect.DeepEqual(record, want) {
			t.Errorf("log record = %v, want %v", record, want)
		}
	})

	t.Run("server error", func(t *testing.T) {
		beginTest(t)
		from := createWallet(t, 1000)
		to := createWallet(t, 0)
		key := newKey()
		port, err := freePort()
		if err != nil {
			t.Fatalf("find free port: %v", err)
		}
		var logs syncBuffer
		server := startServer(t, fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres", port),
			slog.New(slog.NewJSONHandler(&logs, nil)))

		postTransferTo(t, server, transferBody(key, from, to, 100)).errorBody(t, http.StatusInternalServerError)

		record := onlyLogRecord(t, &logs)
		if msg, _ := record["error"].(string); msg == "" {
			t.Errorf("log record has no error message: %v", record)
		}
		delete(record, "error")
		want := map[string]any{
			"level":          "ERROR",
			"msg":            "transfer request",
			"idempotencyKey": key,
			"status":         float64(http.StatusInternalServerError),
		}
		if !reflect.DeepEqual(record, want) {
			t.Errorf("log record = %v, want %v", record, want)
		}
	})
}

// syncBuffer is a bytes.Buffer that a server's handler goroutines and the
// test can share.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// onlyLogRecord parses the single JSON log line in logs, without its time.
func onlyLogRecord(t *testing.T, logs *syncBuffer) map[string]any {
	t.Helper()
	out := strings.TrimSpace(logs.String())
	if out == "" {
		t.Fatal("no log line written")
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d log lines, want 1:\n%s", len(lines), out)
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("parse log line %s: %v", lines[0], err)
	}
	delete(record, "time")
	return record
}
