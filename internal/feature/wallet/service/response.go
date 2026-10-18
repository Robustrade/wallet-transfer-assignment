package service

import (
	"encoding/json"
	"fmt"
	"net/http"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
)

// processedResponse is the body of a successful transfer. Field order matches
// the alphabetical key order of canonicalJSON, so a first response and a
// replay are byte-identical.
type processedResponse struct {
	Status     domain.TransferStatus `json:"status"`
	TransferID int64                 `json:"transferId"`
}

func newProcessedResult(transfer *domain.Transfer) (*TransferResult, error) {
	body, err := json.Marshal(processedResponse{
		Status:     transfer.Status,
		TransferID: transfer.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal transfer response: %w", err)
	}

	return &TransferResult{StatusCode: http.StatusCreated, Body: body}, nil
}

// canonicalJSON re-serialises a stored JSONB body into the exact compact form
// of a first response. Postgres JSONB adds whitespace (and may reorder keys),
// so without this a replay would not be byte-identical to the original.
func canonicalJSON(body []byte) []byte {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return body
	}

	out, err := json.Marshal(v) // map keys are emitted sorted
	if err != nil {
		return body
	}

	return out
}
