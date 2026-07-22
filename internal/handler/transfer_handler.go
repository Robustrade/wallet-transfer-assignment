package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"wallet-transfer/internal/domain"
)

// maxRequestBodyBytes caps the size of a request body (the transfer payload is tiny).
const maxRequestBodyBytes = 1 << 20 // 1 MiB

// handleCreateTransfer enqueues a transfer. Because processing is asynchronous it
// returns 202 Accepted (new) or 200 OK (idempotent replay) with the transfer
// resource; clients poll GET /transfers/{id} for the terminal result.
func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	// Cap the body so an oversized request cannot exhaust memory.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	var req createTransferRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Reject anything after the single JSON object (e.g. "{}{}").
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		respondError(w, http.StatusBadRequest, "request body must contain a single JSON object")
		return
	}

	fromID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "fromWalletId must be a valid uuid")
		return
	}
	toID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "toWalletId must be a valid uuid")
		return
	}

	transfer, created, err := s.transfers.CreateTransfer(r.Context(), domain.NewTransferParams{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromID,
		ToWalletID:     toID,
		Amount:         req.Amount,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	status := http.StatusOK // idempotent replay
	if created {
		status = http.StatusAccepted // new async transfer
		w.Header().Set("Location", "/api/v1/transfers/"+transfer.ID.String())
	}
	respondJSON(w, status, toTransferResponse(transfer))
}

// handleGetTransfer returns the current state of a transfer.
func (s *Server) handleGetTransfer(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		respondError(w, http.StatusBadRequest, "id must be a valid uuid")
		return
	}
	transfer, err := s.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	respondJSON(w, http.StatusOK, toTransferResponse(transfer))
}

// writeServiceError maps an error to a status, logging only unexpected (5xx) ones.
func (s *Server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	status := statusForError(err)
	if status >= http.StatusInternalServerError {
		s.log.Error("request failed",
			zap.String("request_id", requestIDFrom(r.Context())),
			zap.Error(err))
		respondError(w, status, "internal server error")
		return
	}
	respondError(w, status, err.Error())
}
