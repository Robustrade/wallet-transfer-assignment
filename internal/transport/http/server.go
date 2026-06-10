package transporthttp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/application"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
)

const (
	correlationHeader    = "X-Correlation-ID"
	reconcileSuffix      = "/reconcile"
	walletUrlSubPath     = "/wallets/"
	MessageMetNotAllowed = "method not allowed"
)

type correlationIDKey struct{}

type Server struct {
	walletService   *application.WalletService
	transferService *application.TransferService
	healthService   *application.HealthService
}

type createWalletRequest struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

type walletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

type createTransferRequest struct {
	TransferID     string `json:"transferId"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	RequestHash    string `json:"requestHash"`
}

type transferResponse struct {
	TransferID     string `json:"transferId"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	FailureReason  string `json:"failureReason,omitempty"`
}

type apiErrorResponse struct {
	Code          string      `json:"code"`
	Message       string      `json:"message"`
	Details       interface{} `json:"details,omitempty"`
	CorrelationID string      `json:"correlationId,omitempty"`
}

type walletReconciliationResponse struct {
	WalletID      string `json:"walletId"`
	StoredBalance int64  `json:"storedBalance"`
	LedgerBalance int64  `json:"ledgerBalance"`
	Match         bool   `json:"match"`
}

func NewServer(walletService *application.WalletService, transferService *application.TransferService, healthService *application.HealthService) *Server {
	return &Server{
		walletService:   walletService,
		transferService: transferService,
		healthService:   healthService,
	}
}

func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("/wallets", http.HandlerFunc(s.handleWallets))
	mux.Handle("/wallets/", http.HandlerFunc(s.handleWalletByID))
	mux.Handle("/transfers", http.HandlerFunc(s.handleTransfers))
	mux.Handle("/transfers/", http.HandlerFunc(s.handleTransferByID))
	mux.Handle("/health/live", http.HandlerFunc(s.handleLive))
	mux.Handle("/health/ready", http.HandlerFunc(s.handleReady))
	mux.Handle("/openapi.yaml", http.HandlerFunc(s.handleOpenAPISpec))
	mux.Handle("/docs", http.HandlerFunc(s.handleSwaggerUI))
	mux.Handle("/docs/", http.HandlerFunc(s.handleSwaggerUI))
}

// CreateWallet handles POST /wallets.
// @Summary Create a wallet
// @Description Create a new wallet with an initial balance.
// @Tags wallets
// @Accept json
// @Produce json
// @Param X-Correlation-ID header string false "Correlation ID"
// @Param wallet body createWalletRequest true "Wallet payload"
// @Success 201 {object} walletResponse
// @Failure 400 {object} apiErrorResponse
// @Failure 409 {object} apiErrorResponse
// @Router /wallets [post]
func (s *Server) handleCreateWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r.Context(), http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	wallet, err := s.walletService.Create(r.Context(), req.ID, req.Balance)
	if err != nil {
		writeMappedError(w, r.Context(), err)
		return
	}

	writeJSON(w, r.Context(), http.StatusCreated, walletResponse{ID: wallet.ID, Balance: wallet.Balance})
}

func (s *Server) handleWallets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateWallet(w, r)
	default:
		writeError(w, r.Context(), http.StatusMethodNotAllowed, MessageMetNotAllowed, nil)
	}
}

// GetWallet handles GET /wallets/{id}.
// @Summary Retrieve wallet details
// @Description Get a wallet by its ID.
// @Tags wallets
// @Produce json
// @Param X-Correlation-ID header string false "Correlation ID"
// @Param id path string true "Wallet ID"
// @Success 200 {object} walletResponse
// @Failure 400 {object} apiErrorResponse
// @Failure 404 {object} apiErrorResponse
// @Router /wallets/{id} [get]
func (s *Server) handleGetWallet(w http.ResponseWriter, r *http.Request, id string) {
	wallet, err := s.walletService.Get(r.Context(), id)
	if err != nil {
		writeMappedError(w, r.Context(), err)
		return
	}
	writeJSON(w, r.Context(), http.StatusOK, walletResponse{ID: wallet.ID, Balance: wallet.Balance})
}

func (s *Server) handleWalletByID(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, reconcileSuffix) {
		if r.Method != http.MethodGet {
			writeError(w, r.Context(), http.StatusMethodNotAllowed, MessageMetNotAllowed, nil)
			return
		}
		id, ok := parseReconcileID(r.URL.Path, walletUrlSubPath)
		if !ok {
			writeError(w, r.Context(), http.StatusBadRequest, "invalid wallet reconcile path", nil)
			return
		}
		s.handleReconcileWallet(w, r, id)
		return
	}

	if r.Method != http.MethodGet {
		writeError(w, r.Context(), http.StatusMethodNotAllowed, MessageMetNotAllowed, nil)
		return
	}
	id, ok := parseID(r.URL.Path, walletUrlSubPath)
	if !ok {
		writeError(w, r.Context(), http.StatusBadRequest, "invalid wallet id path", nil)
		return
	}
	s.handleGetWallet(w, r, id)
}

func (s *Server) handleReconcileWallet(w http.ResponseWriter, r *http.Request, id string) {
	result, err := s.walletService.Reconcile(r.Context(), id)
	if err != nil {
		writeMappedError(w, r.Context(), err)
		return
	}
	writeJSON(w, r.Context(), http.StatusOK, walletReconciliationResponse{
		WalletID:      result.WalletID,
		StoredBalance: result.StoredBalance,
		LedgerBalance: result.LedgerBalance,
		Match:         result.Match,
	})
}

// CreateTransfer handles POST /transfers.
// @Summary Create a transfer
// @Description Transfer funds from one wallet to another.
// @Tags transfers
// @Accept json
// @Produce json
// @Param X-Correlation-ID header string false "Correlation ID"
// @Param transfer body createTransferRequest true "Transfer payload"
// @Success 201 {object} transferResponse
// @Failure 400 {object} apiErrorResponse
// @Failure 409 {object} apiErrorResponse
// @Failure 404 {object} apiErrorResponse
// @Failure 422 {object} apiErrorResponse
// @Router /transfers [post]
func (s *Server) handleCreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r.Context(), http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Compute canonical request hash server-side to avoid client mistakes.
	canonical := map[string]interface{}{
		"idempotencyKey": req.IdempotencyKey,
		"fromWalletId":   req.FromWalletID,
		"toWalletId":     req.ToWalletID,
		"amount":         req.Amount,
	}
	canonicalJSON, _ := json.Marshal(canonical)
	h := sha256.Sum256(canonicalJSON)
	requestHash := hex.EncodeToString(h[:])

	// capture actor metadata for idempotency records
	createdBy := r.Header.Get("X-User")
	callerIP := ""
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		callerIP = host
	} else {
		callerIP = r.RemoteAddr
	}
	userAgent := r.Header.Get("User-Agent")

	transfer, err := s.transferService.Execute(r.Context(), application.TransferRequest{
		TransferID:     req.TransferID, // service will generate if empty
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		RequestHash:    requestHash,
		CreatedBy:      createdBy,
		CallerIP:       callerIP,
		UserAgent:      userAgent,
	})
	if err != nil {
		writeMappedError(w, r.Context(), err)
		return
	}

	writeJSON(w, r.Context(), http.StatusCreated, transferResponse{
		TransferID:     transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         string(transfer.Status),
		FailureReason:  transfer.FailureReason,
	})
}

func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleCreateTransfer(w, r)
	default:
		writeError(w, r.Context(), http.StatusMethodNotAllowed, MessageMetNotAllowed, nil)
	}
}

// GetTransfer handles GET /transfers/{id}.
// @Summary Retrieve transfer details
// @Description Get a transfer by its ID.
// @Tags transfers
// @Produce json
// @Param X-Correlation-ID header string false "Correlation ID"
// @Param id path string true "Transfer ID"
// @Success 200 {object} transferResponse
// @Failure 400 {object} apiErrorResponse
// @Failure 404 {object} apiErrorResponse
// @Router /transfers/{id} [get]
func (s *Server) handleGetTransfer(w http.ResponseWriter, r *http.Request, id string) {
	transfer, err := s.transferService.TransferByID(r.Context(), id)
	if err != nil {
		writeMappedError(w, r.Context(), err)
		return
	}

	writeJSON(w, r.Context(), http.StatusOK, transferResponse{
		TransferID:     transfer.ID,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID,
		ToWalletID:     transfer.ToWalletID,
		Amount:         transfer.Amount,
		Status:         string(transfer.Status),
		FailureReason:  transfer.FailureReason,
	})
}

func (s *Server) handleTransferByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, r.Context(), http.StatusMethodNotAllowed, MessageMetNotAllowed, nil)
		return
	}
	id, ok := parseID(r.URL.Path, "/transfers/")
	if !ok {
		writeError(w, r.Context(), http.StatusBadRequest, "invalid transfer id path", nil)
		return
	}
	s.handleGetTransfer(w, r, id)
}

func (s *Server) handleLive(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, r.Context(), http.StatusOK, application.HealthResponse{Status: "UP"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.healthService == nil {
		writeError(w, r.Context(), http.StatusServiceUnavailable, "service is not ready", nil)
		return
	}

	response, err := s.healthService.Ready(r.Context())
	if err != nil {
		writeJSON(w, r.Context(), http.StatusServiceUnavailable, response)
		return
	}

	writeJSON(w, r.Context(), http.StatusOK, response)
}

func parseID(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimPrefix(path, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func parseReconcileID(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, reconcileSuffix) {
		return "", false
	}
	trimmed := strings.TrimPrefix(path, prefix)
	id := strings.TrimSuffix(trimmed, reconcileSuffix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

func decodeJSON(r *http.Request, dest any) error {
	if r.Body == nil {
		return errors.New("request body is required")
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dest)
}

func writeJSON(w http.ResponseWriter, ctx context.Context, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	setCorrelationHeader(w, ctx)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, ctx context.Context, status int, message string, details interface{}) {
	writeJSON(w, ctx, status, apiErrorResponse{
		Code:          http.StatusText(status),
		Message:       message,
		Details:       details,
		CorrelationID: correlationIDFromContext(ctx),
	})
}

func writeMappedError(w http.ResponseWriter, ctx context.Context, err error) {
	status := mapErrorToStatus(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = "internal server error"
	}
	writeError(w, ctx, status, message, nil)
}

func mapErrorToStatus(err error) int {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, repository.ErrConflict):
		return http.StatusConflict
	case errors.Is(err, application.ErrIdempotencyConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrEmptyWalletID),
		errors.Is(err, domain.ErrNegativeBalance),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrInvalidWalletID),
		errors.Is(err, domain.ErrInvalidWalletIDFormat),
		errors.Is(err, domain.ErrSameWalletIDs),
		errors.Is(err, domain.ErrInvalidTransferStatus),
		errors.Is(err, domain.ErrInvalidLedgerEntryType),
		errors.Is(err, domain.ErrInvalidLedgerEntries),
		errors.Is(err, domain.ErrLedgerEntrySameWalletID),
		errors.Is(err, domain.ErrLedgerEntryAmountMismatch):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func WithCorrelationID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get(correlationHeader)
		if correlationID == "" {
			correlationID = newCorrelationID()
		}
		ctx := context.WithValue(r.Context(), correlationIDKey{}, correlationID)
		w.Header().Set(correlationHeader, correlationID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func correlationIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}

func setCorrelationHeader(w http.ResponseWriter, ctx context.Context) {
	if id := correlationIDFromContext(ctx); id != "" {
		w.Header().Set(correlationHeader, id)
	}
}

func newCorrelationID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
