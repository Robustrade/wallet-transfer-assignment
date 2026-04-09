package handler

import (
	"encoding/json"
	"net/http"

	"wallet-transfer-assignment/model"
	"wallet-transfer-assignment/service"
)

func TransferHandler(w http.ResponseWriter, r *http.Request) {
	var req model.TransferRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	resp, err := service.ProcessTransfer(r.Context(), req)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	json.NewEncoder(w).Encode(resp)
}
