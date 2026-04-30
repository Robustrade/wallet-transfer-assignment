package dto

// TransferRequestDTO represents the API request for a wallet transfer
type TransferRequestDTO struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   int64  `json:"fromWalletId"`
	ToWalletID     int64  `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// TransferResponseDTO represents the API response for a successful transfer
type TransferResponseDTO struct {
	TransferID int64  `json:"transferId"`
	Status     string `json:"status"`
}

// ErrorResponseDTO represents the API error response
type ErrorResponseDTO struct {
	Status string          `json:"status"`
	Error  ErrorDetailsDTO `json:"error"`
}

// ErrorDetailsDTO contains error details
type ErrorDetailsDTO struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}

// NewErrorResponse creates an error response DTO
func NewErrorResponse(message string, code string) ErrorResponseDTO {
	return ErrorResponseDTO{
		Status: "failure",
		Error: ErrorDetailsDTO{
			Message: message,
			Code:    code,
		},
	}
}
