package responses

type TransferResponse struct {
	TransferID string `json:"transfer_id"`
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
}

type IdResponse struct {
	ID string `json:"id"`
}
