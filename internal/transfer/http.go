package transfer

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type HTTPServer struct {
	service *Service
	metrics *Metrics
}

const maxJSONBodyBytes int64 = 1 << 20

func NewHTTPServer(service *Service) *HTTPServer {
	return NewHTTPServerWithMetrics(service, NewMetrics())
}

func NewHTTPServerWithMetrics(service *Service, metrics *Metrics) *HTTPServer {
	return &HTTPServer{service: service, metrics: metrics}
}

func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /wallets", s.createWallet)
	mux.HandleFunc("GET /wallets", s.getWallets)
	mux.HandleFunc("GET /wallets/", s.getWallet)
	mux.HandleFunc("POST /transfers", s.createTransfer)
	mux.HandleFunc("GET /transfers/", s.getLedger)
	mux.HandleFunc("GET /ledgers", s.getLedgers)
	mux.Handle("GET /metrics", s.metrics.Handler())
	return s.metrics.Middleware(mux)
}

func (s *HTTPServer) createWallet(w http.ResponseWriter, request *http.Request) {
	var wallet Wallet
	if !decodeJSON(w, request, &wallet) || strings.TrimSpace(wallet.ID) == "" || wallet.BalanceCents < 0 {
		writeError(w, http.StatusBadRequest, "id and a non-negative balanceCents are required")
		return
	}
	if err := s.service.CreateWallet(request.Context(), wallet); err != nil {
		status := http.StatusInternalServerError
		message := "failed to create wallet"
		if strings.Contains(err.Error(), "UNIQUE") {
			status = http.StatusConflict
			message = "wallet already exists"
		}
		writeError(w, status, message)
		return
	}
	writeJSON(w, http.StatusCreated, wallet)
}

func (s *HTTPServer) getWallet(w http.ResponseWriter, request *http.Request) {
	wallet, err := s.service.Wallet(request.Context(), strings.TrimPrefix(request.URL.Path, "/wallets/"))
	if errors.Is(err, ErrWalletNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

func (s *HTTPServer) getWallets(w http.ResponseWriter, request *http.Request) {
	wallets, err := s.service.Wallets(request.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wallets)
}

func (s *HTTPServer) createTransfer(w http.ResponseWriter, request *http.Request) {
	var transferRequest TransferRequest
	if !decodeJSON(w, request, &transferRequest) {
		return
	}
	result, created, err := s.service.Transfer(request.Context(), transferRequest)
	s.metrics.ObserveTransfer(result, created, err)
	if err != nil && result.Status == "" {
		status := http.StatusBadRequest
		if errors.Is(err, ErrInsufficientFunds) {
			status = http.StatusConflict
		}
		if errors.Is(err, ErrIdempotencyKeyReuse) {
			status = http.StatusConflict
		}
		if errors.Is(err, ErrWalletNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	if err != nil && errors.Is(err, ErrInsufficientFunds) {
		writeJSON(w, http.StatusConflict, result)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func (m *Metrics) requestMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		started := time.Now()
		responseWriter := &statusResponseWriter{ResponseWriter: w}
		next.ServeHTTP(responseWriter, request)
		status := responseWriter.status
		if status == 0 {
			status = http.StatusOK
		}
		route := request.Pattern
		if route == "" {
			route = request.URL.Path
		}
		m.ObserveRequest(request.Method, route, status, time.Since(started))
	})
}

func (s *HTTPServer) getLedger(w http.ResponseWriter, request *http.Request) {
	transferID := strings.TrimPrefix(request.URL.Path, "/transfers/")
	entries, err := s.service.Ledger(request.Context(), transferID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *HTTPServer) getLedgers(w http.ResponseWriter, request *http.Request) {
	entries, err := s.service.AllLedgers(request.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func decodeJSON(w http.ResponseWriter, request *http.Request, target any) bool {
	request.Body = http.MaxBytesReader(w, request.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(request.Body).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
