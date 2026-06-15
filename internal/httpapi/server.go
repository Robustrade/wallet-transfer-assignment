package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

const maxRequestBodyBytes = 1 << 20

type Server struct {
	transfers *service.TransferService
	logger    *log.Logger
}

type createWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type walletResponse struct {
	ID        string `json:"id"`
	Balance   int64  `json:"balance"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type transferResponse struct {
	ID            string                `json:"id"`
	FromWalletID  string                `json:"fromWalletId"`
	ToWalletID    string                `json:"toWalletId"`
	Amount        int64                 `json:"amount"`
	State         domain.TransferState  `json:"state"`
	ErrorCode     domain.ErrorCode      `json:"errorCode,omitempty"`
	LedgerEntries []ledgerEntryResponse `json:"ledgerEntries,omitempty"`
	CreatedAt     string                `json:"createdAt"`
	UpdatedAt     string                `json:"updatedAt"`
}

type ledgerEntryResponse struct {
	ID         int64                  `json:"id"`
	WalletID   string                 `json:"walletId"`
	TransferID string                 `json:"transferId"`
	Type       domain.LedgerEntryType `json:"type"`
	Amount     int64                  `json:"amount"`
	CreatedAt  string                 `json:"createdAt"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    domain.ErrorCode `json:"code"`
	Message string           `json:"message"`
}

type requestDecodeError struct {
	message string
	cause   error
}

func (e *requestDecodeError) Error() string {
	return e.message
}

func (e *requestDecodeError) Unwrap() error {
	return e.cause
}

func NewServer(transfers *service.TransferService, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	return &Server{
		transfers: transfers,
		logger:    logger,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/health":
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case r.Method == http.MethodPost && r.URL.Path == "/wallets":
		s.handleCreateWallet(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/wallets/"):
		s.handleGetWallet(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/transfers":
		s.handleCreateTransfer(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/transfers/"):
		s.handleGetTransfer(w, r)
	default:
		s.writeError(w, http.StatusNotFound, domain.ErrorInvalidRequest, "route not found")
	}
}

func (s *Server) handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	var request createWalletRequest
	if err := decodeJSON(w, r, &request); err != nil {
		s.writeDecodeError(w, err)
		return
	}

	wallet, err := s.transfers.CreateWallet(r.Context(), domain.CreateWalletInput{
		ID:             request.ID,
		InitialBalance: request.InitialBalance,
	})
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	s.writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}

func (s *Server) handleGetWallet(w http.ResponseWriter, r *http.Request) {
	id, ok := singlePathID(r.URL.Path, "/wallets/")
	if !ok {
		s.writeError(w, http.StatusNotFound, domain.ErrorInvalidRequest, "wallet not found")
		return
	}

	wallet, err := s.transfers.GetWallet(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var request createTransferRequest
	if err := decodeJSON(w, r, &request); err != nil {
		s.writeDecodeError(w, err)
		return
	}

	result, err := s.transfers.CreateTransfer(r.Context(), domain.CreateTransferInput{
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
	})
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	s.writeJSON(w, createTransferStatus(result.Transfer), toTransferResponse(result))
}

func (s *Server) handleGetTransfer(w http.ResponseWriter, r *http.Request) {
	id, ok := singlePathID(r.URL.Path, "/transfers/")
	if !ok {
		s.writeError(w, http.StatusNotFound, domain.ErrorInvalidRequest, "transfer not found")
		return
	}

	result, err := s.transfers.GetTransfer(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, toTransferResponse(result))
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return normalizeDecodeError(err)
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return newRequestDecodeError("request body must contain a single JSON object", err)
	}

	return nil
}

func normalizeDecodeError(err error) error {
	var syntaxError *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	var maxBytesError *http.MaxBytesError

	switch {
	case errors.Is(err, io.EOF):
		return newRequestDecodeError("request body is required", err)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return newRequestDecodeError("invalid JSON", err)
	case errors.As(err, &syntaxError):
		return newRequestDecodeError("invalid JSON", err)
	case errors.As(err, &typeError):
		if typeError.Field != "" {
			return newRequestDecodeError(fmt.Sprintf("invalid value for field %q", typeError.Field), err)
		}
		return newRequestDecodeError("invalid JSON value", err)
	case errors.As(err, &maxBytesError):
		return newRequestDecodeError("request body is too large", err)
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.TrimPrefix(err.Error(), "json: unknown field ")
		return newRequestDecodeError("unknown field "+field, err)
	default:
		return newRequestDecodeError("invalid request body", err)
	}
}

func newRequestDecodeError(message string, cause error) error {
	return &requestDecodeError{message: message, cause: cause}
}

func (s *Server) writeDecodeError(w http.ResponseWriter, err error) {
	s.logger.Printf("decode JSON request: %v", errors.Unwrap(err))
	s.writeError(w, http.StatusBadRequest, domain.ErrorInvalidRequest, err.Error())
}

func (s *Server) writeDomainError(w http.ResponseWriter, err error) {
	var appErr *domain.AppError
	if errors.As(err, &appErr) {
		s.writeError(w, statusForError(appErr.Code), appErr.Code, appErr.Message)
		return
	}

	s.logger.Printf("unexpected error: %v", err)
	s.writeError(w, http.StatusInternalServerError, domain.ErrorPersistenceFailure, "unexpected server error")
}

func (s *Server) writeError(w http.ResponseWriter, status int, code domain.ErrorCode, message string) {
	s.writeJSON(w, status, errorResponse{
		Error: apiError{
			Code:    code,
			Message: message,
		},
	})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		s.logger.Printf("write json response: %v", err)
	}
}

func statusForError(code domain.ErrorCode) int {
	switch code {
	case domain.ErrorInvalidRequest:
		return http.StatusBadRequest
	case domain.ErrorIdempotencyConflict, domain.ErrorWalletAlreadyExists, domain.ErrorInvalidTransferState:
		return http.StatusConflict
	case domain.ErrorWalletNotFound, domain.ErrorTransferNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func createTransferStatus(transfer domain.Transfer) int {
	if transfer.State == domain.TransferProcessed {
		return http.StatusCreated
	}

	switch transfer.ErrorCode {
	case domain.ErrorSourceWalletNotFound, domain.ErrorDestinationWalletNotFound:
		return http.StatusNotFound
	case domain.ErrorInsufficientFunds:
		return http.StatusUnprocessableEntity
	default:
		return http.StatusOK
	}
}

func toWalletResponse(wallet domain.Wallet) walletResponse {
	return walletResponse{
		ID:        wallet.ID,
		Balance:   wallet.Balance,
		CreatedAt: formatTime(wallet.CreatedAt),
		UpdatedAt: formatTime(wallet.UpdatedAt),
	}
}

func toTransferResponse(result domain.TransferResult) transferResponse {
	entries := make([]ledgerEntryResponse, 0, len(result.LedgerEntries))
	for _, entry := range result.LedgerEntries {
		entries = append(entries, ledgerEntryResponse{
			ID:         entry.ID,
			WalletID:   entry.WalletID,
			TransferID: entry.TransferID,
			Type:       entry.Type,
			Amount:     entry.Amount,
			CreatedAt:  formatTime(entry.CreatedAt),
		})
	}

	return transferResponse{
		ID:            result.Transfer.ID,
		FromWalletID:  result.Transfer.FromWalletID,
		ToWalletID:    result.Transfer.ToWalletID,
		Amount:        result.Transfer.Amount,
		State:         result.Transfer.State,
		ErrorCode:     result.Transfer.ErrorCode,
		LedgerEntries: entries,
		CreatedAt:     formatTime(result.Transfer.CreatedAt),
		UpdatedAt:     formatTime(result.Transfer.UpdatedAt),
	}
}

func singlePathID(path string, prefix string) (string, bool) {
	id := strings.TrimPrefix(path, prefix)
	if id == "" || id == path || strings.Contains(id, "/") {
		return "", false
	}

	return id, true
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
