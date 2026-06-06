package domain

import (
	"encoding/json"
	"fmt"
)

// Amount represents a monetary value in the smallest currency unit (e.g. cents).
// Using an integer type avoids floating-point rounding errors that are
// unacceptable in financial calculations.
type Amount int64

// TransferStatus is an integer-coded transfer lifecycle state.
// Storing integers in the database avoids schema changes when display names are
// updated and keeps index size small.
type TransferStatus int

const (
	// TransferStatusPending is the initial state; transfer has been accepted but
	// not yet executed.
	TransferStatusPending TransferStatus = 1

	// TransferStatusProcessed means the transfer completed successfully and the
	// ledger entries have been recorded.
	TransferStatusProcessed TransferStatus = 2

	// TransferStatusFailed means the transfer could not be completed (e.g.
	// insufficient funds or validation error).
	TransferStatusFailed TransferStatus = 3
)

// String returns the human-readable name for the status, used in JSON
// serialisation and log output.
func (s TransferStatus) String() string {
	switch s {
	case TransferStatusPending:
		return "PENDING"
	case TransferStatusProcessed:
		return "PROCESSED"
	case TransferStatusFailed:
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON encodes the status as a human-readable string so that API
// responses carry "PENDING" / "PROCESSED" / "FAILED" rather than raw integers.
func (s TransferStatus) MarshalJSON() ([]byte, error) {
	str := s.String()
	if str == "UNKNOWN" {
		return nil, fmt.Errorf("unknown TransferStatus value: %d", int(s))
	}

	return json.Marshal(str)
}

// UnmarshalJSON decodes a human-readable status string back to its integer
// constant. This is needed so the service can round-trip the cached
// TransferResponse JSON stored in idempotency_records.
func (s *TransferStatus) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}

	switch str {
	case "PENDING":
		*s = TransferStatusPending
	case "PROCESSED":
		*s = TransferStatusProcessed
	case "FAILED":
		*s = TransferStatusFailed
	default:
		return fmt.Errorf("unknown TransferStatus: %q", str)
	}

	return nil
}

// EntryType is an integer-coded ledger entry direction.
type EntryType int

const (
	// EntryTypeDebit records money leaving a wallet.
	EntryTypeDebit EntryType = 1

	// EntryTypeCredit records money entering a wallet.
	EntryTypeCredit EntryType = 2
)

// String returns the human-readable name for the entry type.
func (e EntryType) String() string {
	switch e {
	case EntryTypeDebit:
		return "DEBIT"
	case EntryTypeCredit:
		return "CREDIT"
	default:
		return "UNKNOWN"
	}
}
