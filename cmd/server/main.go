package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/RamG-Gupta/wallet-transfer-assignment/internal/httpapi"
	"github.com/RamG-Gupta/wallet-transfer-assignment/internal/service"
	"github.com/RamG-Gupta/wallet-transfer-assignment/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	dsn := os.Getenv("SQLITE_DSN")
	if dsn == "" {
		dsn = "file:wallet.db"
	}
	st, err := store.Open(dsn)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	defer func() { _ = st.Close() }()

	svc := service.New(st, log)
	srv := httpapi.New(svc)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Info("listening", "addr", addr)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
