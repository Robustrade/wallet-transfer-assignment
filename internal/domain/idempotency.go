package domain

import "time"

// IdempotencyRecord tracks a client-supplied idempotencyKey so that retried
// or duplicated requests can be detected and safely replayed instead of
// re-executing the underlying transfer.
//
// TransferID and ResponseBody are empty until the original request finishes
// processing; a concurrent duplicate arriving while the row exists without a
// TransferID indicates the original request is still in flight.
type IdempotencyRecord struct {
	Key            string
	RequestHash    string
	TransferID     string
	ResponseStatus int
	ResponseBody   []byte
	CreatedAt      time.Time
	CompletedAt    *time.Time
}

// Completed reports whether the original request behind this record has
// finished processing and produced a replayable response.
func (r IdempotencyRecord) Completed() bool {
	return r.TransferID != "" && r.CompletedAt != nil
}
