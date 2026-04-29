package static

const (
	TransferCreated     = "transfer created successfully"
	TransferFetched     = "transfer fetched successfully"
	AllTransfersFetched = "transfers fetched successfully"
	TransferNotFound    = "transfer not found"
	InsufficientBalance = "insufficient balance"
	SameWalletError     = "from and to wallets must be different"
	InvalidAmount       = "amount must be greater than zero"
	WalletNotFound      = "wallet not found"
	MissingFromWallet   = "fromWalletId is required"
	MissingToWallet     = "toWalletId is required"
	StatusPending       = "PENDING"
	StatusProcessed     = "PROCESSED"
	StatusFailed        = "FAILED"
	TypeDebit           = "DEBIT"
	TypeCredit          = "CREDIT"
)
