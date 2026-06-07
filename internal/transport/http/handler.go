package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// maxBodyBytes caps request bodies to protect the server from oversized
// payloads. Transfer/wallet requests are tiny, so 1 MiB is generous.
const maxBodyBytes = 1 << 20

// TransferService is the narrow contract the handlers need for transfers.
// Depending on an interface (not the concrete service) keeps the transport
// layer decoupled and trivially mockable (Interface Segregation).
type TransferService interface {
	CreateTransfer(ctx context.Context, req domain.TransferRequest) (*service.CreateTransferResult, error)
	GetTransfer(ctx context.Context, id string) (*domain.Transfer, error)
}

// WalletService is the narrow contract the handlers need for wallets.
type WalletService interface {
	CreateWallet(ctx context.Context, openingBalance int64, currency string) (*domain.Wallet, error)
	GetWallet(ctx context.Context, id string) (*domain.Wallet, error)
	ListLedger(ctx context.Context, walletID string) ([]domain.LedgerEntry, error)
}

// Handler holds the dependencies for the HTTP API.
type Handler struct {
	transfers TransferService
	wallets   WalletService
	logger    *slog.Logger
}

// NewHandler builds an API handler. A nil logger falls back to slog.Default.
func NewHandler(transfers TransferService, wallets WalletService, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{transfers: transfers, wallets: wallets, logger: logger}
}

// createTransfer handles POST /api/v1/transfers.
//
// It returns 201 Created for a newly processed/failed transfer and 200 OK when
// replaying a previously seen idempotency key, so clients can distinguish a
// fresh action from a deduplicated retry.
func (h *Handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}

	result, err := h.transfers.CreateTransfer(r.Context(), body.toDomain())
	if err != nil {
		h.logError(r, "create transfer", err)
		writeError(w, err)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, newTransferResponse(result.Transfer))
}

// getTransfer handles GET /api/v1/transfers/{id}.
func (h *Handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	transfer, err := h.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransferResponse(transfer))
}

// createWallet handles POST /api/v1/wallets. The server generates the wallet id
// and advertises the new resource's location via the Location header.
func (h *Handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var body createWalletRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}

	wallet, err := h.wallets.CreateWallet(r.Context(), body.OpeningBalance, body.Currency)
	if err != nil {
		h.logError(r, "create wallet", err)
		writeError(w, err)
		return
	}
	w.Header().Set("Location", "/api/v1/wallets/"+wallet.ID)
	writeJSON(w, http.StatusCreated, newWalletResponse(wallet))
}

// getWallet handles GET /api/v1/wallets/{id}.
func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wallet, err := h.wallets.GetWallet(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newWalletResponse(wallet))
}

// listWalletLedger handles GET /api/v1/wallets/{id}/ledger.
func (h *Handler) listWalletLedger(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entries, err := h.wallets.ListLedger(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newLedgerListResponse(id, entries))
}

// health handles GET /healthz for liveness checks.
func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// decodeJSON strictly decodes a JSON request body, rejecting unknown fields and
// malformed payloads with a validation error.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return &domain.ValidationError{Field: "body", Message: "request body is required"}
		}
		return &domain.ValidationError{Field: "body", Message: "invalid JSON body"}
	}
	return nil
}

// logError emits a structured server-side log without leaking internals to the
// client.
func (h *Handler) logError(r *http.Request, op string, err error) {
	h.logger.ErrorContext(r.Context(), "request failed",
		slog.String("op", op),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("error", err.Error()),
	)
}
