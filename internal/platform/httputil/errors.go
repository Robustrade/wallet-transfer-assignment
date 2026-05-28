package httputil

import "net/http"

type ErrorResponse struct {
	Error string `json:"error"`
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	_ = WriteJSON(w, status, ErrorResponse{Error: msg})
}
