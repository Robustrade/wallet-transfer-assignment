package domain

type TransferStatus string
type EntryType string

const (
	Pending   TransferStatus = "PENDING"
	Processed TransferStatus = "PROCESSED"
	Failed    TransferStatus = "FAILED"
)

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type FailureReason string

const (
	InsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"
	InvalidRequest    FailureReason = "INVALID_REQUEST"
	SameWallet        FailureReason = "SAME_WALLET"
)

// -------- TransferStatus Behavior --------

func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	switch s {
	case Pending:
		return next == Processed || next == Failed
	case Processed, Failed:
		return false
	default:
		return false
	}
}

// -------- EntryType Validation --------

func (e EntryType) IsValid() bool {
	return e == Debit || e == Credit
}

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// -------- Transfer --------

type Transfer struct {
	ID             string
	FromWalletID   string
	ToWalletID     string
	Amount         float64
	Status         TransferStatus
	IdempotencyKey string
}

func (t *Transfer) Validate() *ValidationError {
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return &ValidationError{
			Code:    "INVALID_WALLET",
			Message: "wallet id cannot be empty",
		}
	}
	if t.FromWalletID == t.ToWalletID {
		return &ValidationError{
			Code:    "SAME_WALLET",
			Message: "cannot transfer to same wallet",
		}
	}

	return nil
}

// -------- Wallet --------

type Wallet struct {
	ID      string
	Balance float64
}

func (w *Wallet) CanDebit(amount float64) bool {
	return w.Balance >= amount
}
