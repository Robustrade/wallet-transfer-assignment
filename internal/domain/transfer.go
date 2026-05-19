package domain

//go:generate stringer -type=TransferState -linecomment

type TransferState int

const (
	StatePending   TransferState = iota // PENDING
	StateProcessed                      // PROCESSED
	StateFailed                         // FAILED
)

// Transfer is one wallet to wallet move.
type Transfer struct {
	ID            string
	FromWallet    string
	ToWallet      string
	Amount        int64
	State         TransferState
	FailureReason string
}

// CanTransition tells if moving from the current state to next is allowed.
// only pending can move, and only to processed or failed. remaining is blocked.
func (s TransferState) CanTransition(next TransferState) bool {
	if s != StatePending {
		return false
	}
	return next == StateProcessed || next == StateFailed
}
