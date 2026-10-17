package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/config"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func main() {
	log.Println("Starting Robustrade Wallet Transfer Service...")

	// 1. Load configuration from environment
	cfg := config.LoadConfig()

	// 2. Initialize PostgreSQL connection via GORM
	log.Printf("Connecting to database at: %s", maskDatabaseURL(cfg.DatabaseURL))
	db, err := repository.OpenDatabase(cfg.DatabaseURL, cfg.LogLevel)
	if err != nil {
		log.Fatalf("Fatal: could not connect to database: %v", err)
	}

	// 3. Optional AutoMigrate for rapid local prototyping
	if cfg.AutoMigrate {
		log.Println("AUTO_MIGRATE is enabled. Running GORM AutoMigrate...")
		if err := repository.AutoMigrate(db); err != nil {
			log.Fatalf("Fatal: auto migration failed: %v", err)
		}
		log.Println("Auto migration completed successfully.")
	}

	// 4. Initialize architectural layers (Clean Architecture)
	repo := repository.NewGormWalletRepository(db)
	transferService := service.NewTransferService(repo)

	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(transferService)
	healthHandler := handler.NewHealthHandler(repo)

	// 5. Setup routing using standard library net/http ServeMux
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transferHandler.HandleCreateTransfer)
	mux.HandleFunc("GET /transfers/{id}", transferHandler.HandleGetTransfer)
	mux.HandleFunc("GET /wallets/{id}/balance", walletHandler.HandleGetBalance)
	mux.HandleFunc("GET /healthz", healthHandler.HandleHealthz)

	// Wrap with logging middleware
	loggedHandler := loggingMiddleware(mux)

	// 6. Configure HTTP Server with robust timeouts
	server := &http.Server{
		Addr:         cfg.AppAddr,
		Handler:      loggedHandler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	// 7. Start server in a background goroutine
	go func() {
		log.Printf("Server listening on %s", cfg.AppAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server ListenAndServe error: %v", err)
		}
	}()

	// 8. Graceful shutdown on SIGINT or SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited cleanly.")
}

// loggingMiddleware logs incoming HTTP requests with latency.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)
		duration := time.Since(start)
		log.Printf("[%s] %s %s - %d (%v)", r.Method, r.URL.Path, r.RemoteAddr, rw.statusCode, duration)
	})
}

type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *statusResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func maskDatabaseURL(url string) string {
	// Simple masking helper to avoid printing credentials in logs
	if len(url) > 20 {
		return url[:12] + "..." + url[len(url)-10:]
	}
	return "database-url"
}
