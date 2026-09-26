package main

import (
	"context"
	"crypto/tls"
	"errors"
	"log"
	"net/http"
	"time"

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	settings, err := config.Load()
	if err != nil {
		log.Fatalf("load configuration: %v", err)
	}

	repository, err := postgres.Open(context.Background(), settings.DatabaseURL, settings.MaxClaimAttempts)
	if err != nil {
		log.Fatalf("initialize postgres repository: %v", err)
	}
	defer repository.Close()

	server := &http.Server{
		Addr:              ":" + settings.Port,
		Handler:           httpapi.New(service.New(repository)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if settings.SSLEnabled {
		server.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		if settings.TLSCertFile == "" {
			certificate, err := config.DevelopmentCertificate()
			if err != nil {
				log.Fatalf("generate development TLS certificate: %v", err)
			}
			server.TLSConfig.Certificates = []tls.Certificate{certificate}
		}
		log.Printf("wallet transfer service listening with HTTPS on :%s", settings.Port)
		if err := server.ListenAndServeTLS(settings.TLSCertFile, settings.TLSKeyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("wallet transfer service stopped: %v", err)
		}
		return
	}
	log.Printf("wallet transfer service listening with HTTP on :%s", settings.Port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("wallet transfer service stopped: %v", err)
	}
}
