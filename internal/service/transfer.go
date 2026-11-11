package service

import "wallet-transfer-assignment/internal/domain"

// TransferResponse is serialized by the workflow before committing its terminal
// result. The handler must replay the persisted bytes instead of re-encoding it.
type TransferResponse struct {
	TransferID    string                `json:"transferId"`
	Status        domain.TransferStatus `json:"status"`
	FailureReason string                `json:"failureReason,omitempty"`
}

// TransferResult crosses the service/HTTP boundary as a replayable snapshot.
// Pending results are transient and must never be stored as terminal responses.
type TransferResult struct {
	StatusCode int
	Body       []byte
	Replayed   bool
}
