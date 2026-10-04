package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/app/transfer"
	"github.com/amantiwari/wallet-transfer/internal/app/wallet"
	"github.com/amantiwari/wallet-transfer/internal/domain"
	"github.com/amantiwari/wallet-transfer/internal/platform/requestid"
)

const maxBodyBytes = 1 << 20

// WalletAPI is the wallet use case consumed by HTTP handlers.
type WalletAPI interface {
	Create(ctx context.Context, cmd wallet.CreateCommand) (domain.Wallet, error)
	Get(ctx context.Context, id string) (domain.Wallet, error)
}

// TransferAPI is the transfer use case consumed by HTTP handlers.
type TransferAPI interface {
	Execute(ctx context.Context, cmd transfer.Command) (transfer.Result, error)
	Get(ctx context.Context, id string) (domain.Transfer, error)
	Ledger(ctx context.Context, transferID string) ([]domain.LedgerEntry, error)
}

// Pinger reports database availability.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Dependencies are the inbound adapter's collaborators.
type Dependencies struct {
	Wallets        WalletAPI
	Transfers      TransferAPI
	DB             Pinger
	Logger         *slog.Logger
	RequestTimeout time.Duration
}

type handler struct {
	wallets   WalletAPI
	transfers TransferAPI
	db        Pinger
	log       *slog.Logger
}

// NewRouter builds the HTTP handler with middleware applied.
func NewRouter(deps Dependencies) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	h := &handler{
		wallets:   deps.Wallets,
		transfers: deps.Transfers,
		db:        deps.DB,
		log:       deps.Logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /wallets", h.createWallet)
	mux.HandleFunc("GET /wallets/{walletId}", h.getWallet)
	mux.HandleFunc("POST /transfers", h.createTransfer)
	mux.HandleFunc("GET /transfers/{transferId}", h.getTransfer)
	mux.HandleFunc("GET /transfers/{transferId}/ledger", h.getLedger)

	var root http.Handler = mux
	root = recoverPanics(deps.Logger, root)
	root = accessLog(deps.Logger, root)
	root = timeout(deps.RequestTimeout)(root)
	root = requestID(root)
	return root
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if err := h.db.Ping(r.Context()); err != nil {
		h.log.ErrorContext(r.Context(), "health check failed",
			"request_id", requestid.From(r.Context()),
			"error", err.Error(),
		)
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "database is unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) createWallet(w http.ResponseWriter, r *http.Request) {
	var body createWalletRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	cmd := wallet.CreateCommand{ID: body.ID}
	if body.InitialBalance != nil {
		cmd.InitialBalance = int64(*body.InitialBalance)
	}
	created, err := h.wallets.Create(r.Context(), cmd)
	if err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, newWalletResponse(created))
}

func (h *handler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.Get(r.Context(), r.PathValue("walletId"))
	if err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newWalletResponse(wallet))
}

func (h *handler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := decodeJSON(w, r, &body); err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	if body.Amount == nil {
		h.writeMappedError(w, r, &domain.ValidationError{Details: []domain.FieldError{{
			Field:   "amount",
			Message: "is required",
		}}})
		return
	}
	result, err := h.transfers.Execute(r.Context(), transfer.Command{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         int64(*body.Amount),
	})
	if err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	h.writeTransfer(w, r, result)
}

func (h *handler) getTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, err := h.transfers.Get(r.Context(), r.PathValue("transferId"))
	if err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, newTransferResponse(transfer))
}

func (h *handler) getLedger(w http.ResponseWriter, r *http.Request) {
	transferID := r.PathValue("transferId")
	entries, err := h.transfers.Ledger(r.Context(), transferID)
	if err != nil {
		h.writeMappedError(w, r, err)
		return
	}
	payload := ledgerResponse{TransferID: transferID, Entries: make([]ledgerEntryResponse, 0, len(entries))}
	for _, entry := range entries {
		payload.Entries = append(payload.Entries, ledgerEntryResponse{
			ID:         entry.ID,
			WalletID:   entry.WalletID,
			TransferID: entry.TransferID,
			Type:       string(entry.Type),
			Amount:     entry.Amount,
			CreatedAt:  entry.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *handler) writeTransfer(w http.ResponseWriter, r *http.Request, result transfer.Result) {
	switch result.Transfer.Status {
	case domain.StatusProcessed:
		writeJSON(w, http.StatusCreated, newTransferResponse(result.Transfer))
	case domain.StatusFailed:
		writeJSON(w, http.StatusUnprocessableEntity, newTransferResponse(result.Transfer))
	default:
		h.log.ErrorContext(r.Context(), "transfer left in unexpected status",
			"request_id", requestid.From(r.Context()),
			"transfer_id", result.Transfer.ID,
			"status", result.Transfer.Status,
		)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal, "an unexpected error occurred", nil)
	}
}

func (h *handler) writeMappedError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message, details, ok := mapClientError(err)
	if !ok {
		h.log.ErrorContext(r.Context(), "request failed",
			"request_id", requestid.From(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"error", err.Error(),
		)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal, "an unexpected error occurred", nil)
		return
	}
	writeError(w, status, code, message, details)
}

type createWalletRequest struct {
	ID             string       `json:"id"`
	InitialBalance *strictInt64 `json:"initialBalance"`
}

type createTransferRequest struct {
	IdempotencyKey string       `json:"idempotencyKey"`
	FromWalletID   string       `json:"fromWalletId"`
	ToWalletID     string       `json:"toWalletId"`
	Amount         *strictInt64 `json:"amount"`
}

type strictInt64 int64

func (n *strictInt64) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return errors.New("must be an integer")
	}
	if data[0] == '"' || bytes.ContainsAny(data, ".eE+") {
		return errors.New("must be an integer")
	}
	var value int64
	if err := json.Unmarshal(data, &value); err != nil {
		return errors.New("must be an integer")
	}
	*n = strictInt64(value)
	return nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType := r.Header.Get("Content-Type")
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil || parsed != "application/json" {
		return unsupportedMediaError{}
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &domain.ValidationError{Details: []domain.FieldError{{
				Field:   "body",
				Message: "must be at most 1MB",
			}}}
		}
		return &domain.ValidationError{Details: []domain.FieldError{{
			Field:   "body",
			Message: "could not be read",
		}}}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return &domain.ValidationError{Details: []domain.FieldError{{
			Field:   "body",
			Message: "is required",
		}}}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return &domain.ValidationError{Details: []domain.FieldError{{
			Field:   "body",
			Message: jsonDecodeMessage(err),
		}}}
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return &domain.ValidationError{Details: []domain.FieldError{{
			Field:   "body",
			Message: "must be a single JSON value",
		}}}
	}
	return nil
}

func jsonDecodeMessage(err error) string {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) && typeErr.Field != "" {
		return typeErr.Field + " has an invalid type"
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "must be an integer"):
		return "numeric fields must be integers"
	case strings.Contains(message, "unknown field"):
		return "contains an unknown field"
	default:
		return "must be valid JSON"
	}
}

type unsupportedMediaError struct{}

func (unsupportedMediaError) Error() string { return "content type must be application/json" }
