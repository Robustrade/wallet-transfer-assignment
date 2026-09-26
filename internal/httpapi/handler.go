package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type Handler struct {
	service *service.Service
}

func New(transferService *service.Service) http.Handler {
	handler := &Handler{service: transferService}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", handler.index)
	mux.HandleFunc("POST /wallets", handler.createWallet)
	mux.HandleFunc("GET /wallets/{walletId}", handler.getWallet)
	mux.HandleFunc("GET /wallets/{walletId}/transfers", handler.listTransfers)
	mux.HandleFunc("POST /transfers", handler.createTransfer)
	mux.HandleFunc("GET /healthz", handler.health)
	return mux
}

func (h *Handler) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "wallet-transfer-service",
		"endpoints": []map[string]string{
			{"method": http.MethodPost, "path": "/wallets", "description": "Create a wallet"},
			{"method": http.MethodGet, "path": "/wallets/{walletId}", "description": "Get a wallet balance"},
			{"method": http.MethodGet, "path": "/wallets/{walletId}/transfers", "description": "Get wallet transfer history"},
			{"method": http.MethodPost, "path": "/transfers", "description": "Create an idempotent wallet transfer"},
			{"method": http.MethodGet, "path": "/healthz", "description": "Check service health"},
		},
	})
}

func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var request struct {
		WalletID       string `json:"walletId"`
		InitialBalance int64  `json:"initialBalance"`
	}
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wallet, err := h.service.CreateWallet(r.Context(), request.WalletID, request.InitialBalance)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletExists):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, domain.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("create wallet failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, wallet)
}

func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.service.GetWallet(r.Context(), r.PathValue("walletId"))
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, domain.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("get wallet failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

func (h *Handler) listTransfers(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be an integer between 1 and 100")
			return
		}
		limit = parsed
	}
	transfers, err := h.service.ListTransfers(r.Context(), r.PathValue("walletId"), limit)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, domain.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("list wallet transfers failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"transfers": transfers})
}

func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var request domain.TransferRequest
	if err := decodeJSON(w, r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	result, err := h.service.Transfer(r.Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, domain.ErrIdempotencyConflict):
			writeError(w, http.StatusConflict, "idempotency_key_conflict")
		case errors.Is(err, domain.ErrIdempotencyInProgress):
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusConflict, "idempotency_in_progress")
		case errors.Is(err, domain.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			log.Printf("transfer failed: %v", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, result.HTTPStatus, result.Transfer)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write HTTP response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
