package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/service"
)

// WalletHandler binds the wallet routes (POST /wallets,
// GET /wallets/{id}, GET /wallets/{id}/transfers) to the service
// layer.
type WalletHandler struct {
	wsvc *service.WalletService
	tsvc *service.TransferService
}

// NewWalletHandler wires the handler to its service dependencies.
// Both arguments must be non-nil. `tsvc` is used only by the
// per-wallet transfer history endpoint; passing it to the wallet
// handler (rather than mounting a separate TransferListHandler)
// keeps the route grouping natural under /wallets/{id}/...
func NewWalletHandler(wsvc *service.WalletService, tsvc *service.TransferService) *WalletHandler {
	return &WalletHandler{wsvc: wsvc, tsvc: tsvc}
}

// walletResponse is the wire shape of GET /wallets/{id} and the
// POST /wallets response body. Balance is returned in minor units
// to match how it's stored.
type walletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

func toWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{ID: w.ID, Balance: w.Balance}
}

// Get handles GET /wallets/{id}. The {id} path parameter comes from
// Go 1.22+ ServeMux's pattern matching (r.PathValue).
//
// Returns 200 with the wallet on success, 404 when the wallet does
// not exist (mapped via writeError from repository.ErrNotFound).
func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wallet, err := h.wsvc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

// ListTransfers handles GET /wallets/{id}/transfers, returning the
// per-wallet transfer history with cursor-based pagination.
//
// Query parameters:
//
//   - limit  (optional, default 50, max 200)
//   - before (optional, RFC 3339 timestamp; from the previous page's
//     nextCursor)
//
// We do NOT 404 when the wallet has zero transfers — a wallet that
// exists with no history is a normal state, and returning an empty
// list is more useful than an error. To avoid leaking the existence
// of arbitrary wallet ids, we also do not 404 for unknown wallets;
// a non-existent wallet simply has zero transfers. (Authorization
// would change this if added later.)
func (h *WalletHandler) ListTransfers(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("id")
	limit, before, err := parseListQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: err.Error()})
		return
	}

	rows, next, err := h.tsvc.ListTransfersByWallet(r.Context(), walletID, limit, before)
	if err != nil {
		// Special-case empty-wallet-id (which the service returns as
		// ErrWalletNotFound) so callers see a 400 rather than the
		// 404 mapping that would imply "we looked it up and it's
		// missing".
		if errors.Is(err, domain.ErrWalletNotFound) && walletID == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{Error: "wallet id is required"})
			return
		}
		writeError(w, r, err)
		return
	}

	resp := transferListResponse{
		Transfers: make([]transferResponse, 0, len(rows)),
	}
	for i := range rows {
		resp.Transfers = append(resp.Transfers, toTransferResponse(&rows[i]))
	}
	if next != nil {
		resp.NextCursor = next.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, resp)
}

// createWalletRequest is the wire shape of POST /wallets. Balance is
// the wallet's opening balance in minor units.
type createWalletRequest struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

// Create handles POST /wallets. This is a convenience endpoint for
// seeding wallets in tests and demos; in production it would live
// behind an admin scope (see AGENTS.md §S-10).
//
// 201 Created is returned with the seeded wallet on success.
// Validation errors map to 400; duplicate ids surface as 500 via
// the unique-violation fallthrough in writeError.
func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorBody{Error: "invalid json: " + err.Error()})
		return
	}
	wallet, err := h.wsvc.Create(r.Context(), req.ID, req.Balance)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}
