package domain

import (
	"testing"
	"time"
)

func TestIdempotencyRecordCompleted(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name         string
		record       IdempotencyRecord
		wantComplete bool
	}{
		{name: "in flight", record: IdempotencyRecord{}, wantComplete: false},
		{name: "missing completion time", record: IdempotencyRecord{TransferID: "transfer-1"}, wantComplete: false},
		{name: "completed", record: IdempotencyRecord{TransferID: "transfer-1", CompletedAt: &now}, wantComplete: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.record.Completed(); got != tt.wantComplete {
				t.Fatalf("Completed() = %v, want %v", got, tt.wantComplete)
			}
		})
	}
}
