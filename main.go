package main

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/handler"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/Robustrade/wallet-transfer-assignment/service"
)

func main() {
	db, err := connectDB()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = db.Close()
	}()

	walletRepo := repository.NewWalletRepository(db)
	transferRepo := repository.NewTransferRepository(db)
	ledgerRepo := repository.NewLedgerRepository(db)

	transferService := service.NewTransferService(
		db,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	transferHandler := handler.NewTransferHandler(
		transferService,
	)

	mux := http.NewServeMux()

	mux.HandleFunc(
		"/transfers",
		transferHandler.CreateTransfer,
	)

	handler := recoveryMiddleware(
		requestIDMiddleware(
			loggingMiddleware(mux),
		),
	)

	server := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Println("Server running on :8080")

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")

		if requestID == "" {
			requestID = uuid.New().String()
		}

		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		next.ServeHTTP(w, r)

		log.Printf(
			"method=%s path=%s duration=%s",
			r.Method,
			r.URL.Path,
			time.Since(start),
		)
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic: %v", recovered)

				http.Error(
					w,
					"internal server error",
					http.StatusInternalServerError,
				)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
