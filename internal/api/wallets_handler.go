package api

import (
	"net/http"
	"strconv"
)

func (server *Server) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	result, err := server.wallets.GetWallet(r.Context(), pathValue(r, "id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) handleGetWalletLedger(w http.ResponseWriter, r *http.Request) {
	limit, err := parseOptionalInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "INVALID_LIMIT"})
		return
	}
	cursor, err := parseOptionalInt64(r.URL.Query().Get("cursor"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "INVALID_CURSOR"})
		return
	}

	result, err := server.wallets.ListLedger(r.Context(), pathValue(r, "id"), limit, cursor)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": result})
}

func parseOptionalInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func parseOptionalInt64(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}
