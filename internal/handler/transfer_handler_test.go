package handler

import (
	"net/http"
	"testing"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

// TestTransferStatusCode guards against a replay silently changing a
// transfer's apparent HTTP outcome. A FAILED transfer must return 422
// whether it's being reported for the first time or replayed from an
// idempotency key -- Replayed and Status are independent facts, and the
// status code must be decided by Status first.
func TestTransferStatusCode(t *testing.T) {
	cases := []struct {
		name   string
		result *service.TransferResult
		want   int
	}{
		{"fresh processed", &service.TransferResult{Status: domain.TransferProcessed, Replayed: false}, http.StatusCreated},
		{"fresh failed", &service.TransferResult{Status: domain.TransferFailed, Replayed: false}, http.StatusUnprocessableEntity},
		{"replayed processed", &service.TransferResult{Status: domain.TransferProcessed, Replayed: true}, http.StatusOK},
		{"replayed failed", &service.TransferResult{Status: domain.TransferFailed, Replayed: true}, http.StatusUnprocessableEntity},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := transferStatusCode(tc.result); got != tc.want {
				t.Errorf("transferStatusCode(%+v) = %d, want %d", tc.result, got, tc.want)
			}
		})
	}
}
