package domain

// TransferStatus is the transfer state machine. Allowed transitions:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// allowedTransitions maps each state to the states it may move to. Terminal
// states (PROCESSED, FAILED) have no outgoing transitions, which makes the
// state machine safe under retries: a duplicate attempt cannot move a
// completed transfer again.
var allowedTransitions = map[TransferStatus][]TransferStatus{
	StatusPending: {StatusProcessed, StatusFailed},
}

// CanTransitionTo reports whether moving from the current status to next is
// allowed by the state machine.
func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	for _, allowed := range allowedTransitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// Transfer is a single wallet-to-wallet movement of funds.
type Transfer struct {
	ID           string
	FromWalletID string
	ToWalletID   string
	Amount       int64
	Status       TransferStatus
}
