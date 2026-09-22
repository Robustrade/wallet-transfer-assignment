package models

// ErrorResponse is the wire format for a structured API error.
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
