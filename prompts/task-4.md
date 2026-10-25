# AGENT ROLE & RESPONSIBILITIES

### Role
You are an autonomous Senior Go Backend Engineer and Transport Protocol Specialist executing inside the Antigravity CLI environment[cite: 1]. You implement clean, robust REST HTTP handlers, strict input validation, precise HTTP status mappings, structured JSON responses, and end-to-end server orchestration[cite: 1, 2].

### Core Responsibilities
1. **Contract & Schema Alignment**: Strictly adhere to the `ASSIGNMENT.md` specification[cite: 1]:
   - Terminology: `wallets` (never `accounts`)[cite: 1].
   - Payload keys: `fromWalletId`, `toWalletId`, `amount`, and `idempotencyKey` directly within the JSON request body[cite: 1].
   - Numeric precision: Amounts in `int64` minor units (cents)[cite: 1, 2]. Floating-point numbers (`float32`, `float64`) are strictly forbidden[cite: 1, 2].
2. **HTTP Endpoints & Contract Mapping**:
   - `POST /api/v1/wallets`: Create a new wallet (`{"userId": "...", "currency": "USD"}`)[cite: 1, 2]. Returns `201 Created`[cite: 1, 2].
   - `GET /api/v1/wallets/{id}`: Fetch wallet by ID. Returns `200 OK` or `404 Not Found`[cite: 1, 2].
   - `POST /api/v1/transfers`: Orchestrate inter-wallet transfers[cite: 1, 2]. Payload carries `idempotencyKey` in the JSON body (optional header fallback supported)[cite: 1]. Returns `200 OK` on successful transfer or idempotent replay[cite: 1, 2].
3. **Domain Error Translation**: Map internal sentinel domain errors to deterministic HTTP status codes[cite: 1]:
   - `domain.ErrWalletNotFound` $\rightarrow$ `404 Not Found`[cite: 1]
   - `domain.ErrInsufficientFunds` $\rightarrow$ `422 Unprocessable Entity`[cite: 1]
   - `domain.ErrCurrencyMismatch` $\rightarrow$ `400 Bad Request`[cite: 1]
   - `domain.ErrSelfTransfer` $\rightarrow$ `400 Bad Request`[cite: 1]
   - `domain.ErrInvalidAmount` $\rightarrow$ `400 Bad Request`[cite: 1]
   - `domain.ErrIdempotencyKeyRequired` $\rightarrow$ `400 Bad Request`[cite: 1]
   - `domain.ErrConcurrentRequest` $\rightarrow$ `409 Conflict`[cite: 1]
   - `domain.ErrIdempotencyKeyConflict` $\rightarrow$ `422 Unprocessable Entity`[cite: 1]
4. **Clean Decoupling**: Keep handlers thin by delegating business logic and transaction management to `WalletService`[cite: 1, 2].
5. **Compilation Verification**: Conclude by running validation commands (`go vet ./...` and `make build`) to verify that `bin/server` compiles cleanly with zero warnings[cite: 1].

---

# TASK: Milestone 4 - Transport Layer, REST Handlers, Routing & Main Server

### 1. WHAT (Files to Create / Update)
Implement HTTP JSON transport handlers, route registration using the standard library `net/http` ServeMux (Go 1.22+ pattern matching), error translation helper, and the runnable server entry point in `cmd/server/main.go`[cite: 1].

#### Target Files:
1. `internal/transport/http/response.go`
2. `internal/transport/http/handler.go`
3. `internal/transport/http/routes.go`
4. `cmd/server/main.go` (replace stub)

---

### 2. WHY
- **Contract Adherence**: Public interfaces must match the automated grader suite and assignment expectations (`fromWalletId`, `toWalletId`, `idempotencyKey` in JSON body)[cite: 1].
- **Deterministic Error Mapping**: Clients must receive appropriate HTTP statuses (`409` for in-flight collisions, `422` for overdrafts/payload hash conflicts, `404` for missing wallets) rather than generic `500` errors[cite: 1].
- **Application Bootstrap**: Wires SQLite configuration, migrations, repositories, and services into an executable HTTP daemon[cite: 1].

---

### 3. WHEN
Execute immediately after **Milestone 3**[cite: 1] (when the domain entities, SQLite repositories, and service business logic pass all unit tests[cite: 1]).

---

### 4. HOW (Exact File Implementations)

#### Step 4.1: Create `internal/transport/http/response.go`
Define standardized JSON response helpers:

```go
package http

import (
	"encoding/json"
	"net/http"
)

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

func writeError(w http.ResponseWriter, status int, errMessage string) {
	writeJSON(w, status, ErrorResponse{
		Error:   http.StatusText(status),
		Message: errMessage,
	})
}

```

---

#### Step 4.2: Create `internal/transport/http/handler.go`

Implement thin REST handlers that parse inputs, handle fallback `Idempotency-Key` headers, and map domain errors:

```go
package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/domain](https://github.com/Robustrade/wallet-transfer-assignment/internal/domain)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/service](https://github.com/Robustrade/wallet-transfer-assignment/internal/service)"
)

type WalletHandler struct {
	svc service.WalletService
}

func NewWalletHandler(svc service.WalletService) *WalletHandler {
	return &WalletHandler{svc: svc}
}

type CreateWalletRequest struct {
	UserID   string `json:"userId"`
	Currency string `json:"currency"`
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.UserID) == "" || strings.TrimSpace(req.Currency) == "" {
		writeError(w, http.StatusBadRequest, "userId and currency are required")
		return
	}

	wallet, err := h.svc.CreateWallet(r.Context(), req.UserID, strings.ToUpper(req.Currency))
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	walletID := r.PathValue("id")
	if walletID == "" {
		writeError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	wallet, err := h.svc.GetWallet(r.Context(), walletID)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}

func (h *WalletHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	var req service.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json payload")
		return
	}

	// Sourced primarily from body as per ASSIGNMENT.md; header acts as clean fallback
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}

	if req.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "idempotencyKey is required")
		return
	}

	resp, err := h.svc.Transfer(r.Context(), req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *WalletHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, domain.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrConcurrentRequest):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domain.ErrCurrencyMismatch),
		errors.Is(err, domain.ErrSelfTransfer),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrIdempotencyKeyRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

```

---

#### Step 4.3: Create `internal/transport/http/routes.go`

Wire routes using standard Go `http.ServeMux`:

```go
package http

import (
	"net/http"
)

func RegisterRoutes(handler *WalletHandler) *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// V1 Wallets and Transfers Endpoints
	mux.HandleFunc("POST /api/v1/wallets", handler.CreateWallet)
	mux.HandleFunc("GET /api/v1/wallets/{id}", handler.GetWallet)
	mux.HandleFunc("POST /api/v1/transfers", handler.Transfer)

	return mux
}

```

---

#### Step 4.4: Update `cmd/server/main.go`

Wire database initialization, repositories, services, and HTTP routing into the production entry point:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"[github.com/Robustrade/wallet-transfer-assignment/internal/repository](https://github.com/Robustrade/wallet-transfer-assignment/internal/repository)"
	"[github.com/Robustrade/wallet-transfer-assignment/internal/service](https://github.com/Robustrade/wallet-transfer-assignment/internal/service)"
	transporthttp "[github.com/Robustrade/wallet-transfer-assignment/internal/transport/http](https://github.com/Robustrade/wallet-transfer-assignment/internal/transport/http)"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Database initialization
	dsn := os.Getenv("DATABASE_PATH")
	if dsn == "" {
		dsn = "wallet.db"
	}

	slog.Info("Initializing SQLite connection pool", "dsn", dsn)
	db, err := repository.NewSQLiteDB(dsn)
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Layer dependency injection
	txManager := repository.NewSQLiteTxManager(db)
	walletRepo := repository.NewSQLiteWalletRepository(db)
	transferRepo := repository.NewSQLiteTransferRepository(db)
	idempotencyRepo := repository.NewSQLiteIdempotencyRepository(db)

	walletService := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)
	handler := transporthttp.NewWalletHandler(walletService)
	router := transporthttp.RegisterRoutes(handler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown handling
	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("Wallet Transfer Service running", "addr", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("Server encountered fatal error", "error", err)
			os.Exit(1)
		}
	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			slog.Error("Failed to stop server gracefully", "error", err)
			_ = server.Close()
		}
	}

	slog.Info("Server stopped cleanly")
}

```

---

### 5. CLI EXECUTION & VERIFICATION PROTOCOL

Run these validation commands to verify the setup:

1. Re-evaluate dependencies and tidy:

```bash
go mod tidy

```

2. Run compiler type check and syntax vetting:

```bash
go vet ./...

```

3. Build the server binary using the project Makefile:

```bash
make build

```

4. Verify server binary execution with immediate exit:

```bash
./bin/server &
PID=\$!
sleep 1
curl -s http://localhost:8080/health
kill \$PID

```

**Success Criteria**:

* `go vet ./...` completes with 0 warnings or errors.
* `make build` compiles `bin/server` with exit code 0.


* `curl http://localhost:8080/health` returns `{"status":"ok"}`.
* Handlers cleanly consume `idempotencyKey`, `fromWalletId`, and `toWalletId` directly from the request JSON body.