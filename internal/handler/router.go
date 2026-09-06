package handler

import "net/http"

func NewRouter(transferHandler *TransferHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /transfers", func(w http.ResponseWriter, r *http.Request) {
		transferHandler.CreateTransfer(w, *r)
	})

	return mux
}
