package domain

// allowedTransitions defines the valid transfer state machine:
//
//	PENDING -> PROCESSED
//	PENDING -> FAILED
//
// Terminal states (PROCESSED, FAILED) have no outgoing transitions.
var allowedTransitions = map[TransferStatus]map[TransferStatus]bool{
	StatusPending: {
		StatusProcessed: true,
		StatusFailed:    true,
	},
}

// CanTransitionTo reports whether the transfer may move to the target status.
func (t Transfer) CanTransitionTo(target TransferStatus) bool {
	return allowedTransitions[t.Status][target]
}

// TransitionTo moves the transfer to target if allowed, otherwise returns
// ErrInvalidStateTransition and leaves the status unchanged.
func (t *Transfer) TransitionTo(target TransferStatus) error {
	if !t.CanTransitionTo(target) {
		return ErrInvalidStateTransition
	}
	t.Status = target
	return nil
}

// ValidateTransferRequest enforces the pure business rules for a transfer
// request, independent of any persistence concerns.
func ValidateTransferRequest(fromWalletID, toWalletID string, amount int64) error {
	if fromWalletID == "" || toWalletID == "" {
		return ErrMissingWallet
	}
	if fromWalletID == toWalletID {
		return ErrSameWallet
	}
	if amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}