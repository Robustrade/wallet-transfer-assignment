package domain

type TransferStatus string

const (
	PENDING   TransferStatus = "PENDING"
	PROCESSED TransferStatus = "PROCESSED"
	FAILED    TransferStatus = "FAILED"
)
