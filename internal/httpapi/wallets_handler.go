package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

// WalletService is the subset of service.WalletService this handler needs.
type WalletService interface {
	Create(ctx context.Context, id, currency string) (domain.Wallet, error)
	Get(ctx context.Context, id string) (domain.Wallet, error)
	ListEntries(ctx context.Context, walletID string, limit int) ([]domain.LedgerEntry, error)
}

type walletsHandler struct {
	wallets WalletService
}

// Create handles POST /wallets.
func (h *walletsHandler) Create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	var req createWalletRequest
	if err := dec.Decode(&req); err != nil {
		writeMalformed(w, "request body is not a valid wallet request: "+err.Error())
		return
	}
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		writeMalformed(w, "request body must contain a single JSON object")
		return
	}

	wallet, err := h.wallets.Create(r.Context(), req.ID, req.Currency)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newWalletResponse(wallet))
}

// Get handles GET /wallets/{id}.
func (h *walletsHandler) Get(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newWalletResponse(wallet))
}

const (
	defaultEntriesLimit = 20
	maxEntriesLimit     = 100
)

// ListEntries handles GET /wallets/{id}/entries — the wallet's most recent
// ledger activity, newest first. No pagination: this is an optional
// enhancement per ASSIGNMENT.md, and "most recent N" covers it without a
// cursor to maintain.
func (h *walletsHandler) ListEntries(w http.ResponseWriter, r *http.Request) {
	limit := defaultEntriesLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > maxEntriesLimit {
			writeValidationError(w, "limit", "limit must be a positive integer no greater than 100")
			return
		}
		limit = n
	}

	entries, err := h.wallets.ListEntries(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeDomainError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newLedgerEntriesResponse(entries))
}
