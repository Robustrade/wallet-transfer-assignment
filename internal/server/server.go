package server

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gerry1818/wallet-transfer-assignment/internal/db"
	"github.com/gerry1818/wallet-transfer-assignment/internal/handler"
	"github.com/gerry1818/wallet-transfer-assignment/internal/logger"
	"github.com/gerry1818/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/gerry1818/wallet-transfer-assignment/internal/service"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// Config holds server configuration
type Config struct {
	Port            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	DatabaseURL     string
	ShutdownTimeout time.Duration
}

// DefaultConfig returns default server configuration
func DefaultConfig() Config {
	return Config{
		Port:            ":8080",
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     10 * time.Second,
		DatabaseURL:     "postgres://postgres:postgres@localhost:5432/wallet?sslmode=disable",
		ShutdownTimeout: 5 * time.Second,
	}
}

// Server represents the application server
type Server struct {
	httpServer *http.Server
	database   *gorm.DB
	config     Config
}

var openDB = db.NewDB
var signalNotify = signal.Notify

// New creates a new server instance
func New(cfg Config) (*Server, error) {
	logger.Log.Info("initializing server")

	// Initialize GORM database connection
	database, err := openDB(cfg.DatabaseURL)
	if err != nil {
		logger.Log.Error("failed to connect to database", zap.Error(err))
		return nil, err
	}

	// Check database connection
	sqlDB, err := database.DB()
	if err != nil {
		logger.Log.Error("failed to get database connection", zap.Error(err))
		return nil, err
	}

	if err := sqlDB.Ping(); err != nil {
		logger.Log.Error("database not reachable", zap.Error(err))
		return nil, err
	}

	logger.Log.Info("connected to database")

	// Wire dependencies
	repo := postgres.NewRepo(database)
	svc := service.NewTransferService(repo)
	h := handler.NewHandler(svc)

	// Routes
	mux := http.NewServeMux()
	// Versioned API routes (preferred)
	mux.HandleFunc("/api/v1/health", h.Health)
	mux.HandleFunc("/api/v1/transfers", h.Transfer)
	// Backward-compatible routes
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/transfers", h.Transfer)
	mux.Handle("/metrics", promhttp.Handler())

	// HTTP server with timeouts
	httpServer := &http.Server{
		Addr:         cfg.Port,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	return &Server{
		httpServer: httpServer,
		database:   database,
		config:     cfg,
	}, nil
}

// Start starts the server and blocks until shutdown signal is received
func (s *Server) Start() error {
	// Start server in goroutine
	go func() {
		logger.Log.Info("server started", zap.String("port", s.config.Port))
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log.Fatal("server failed", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signalNotify(quit, syscall.SIGINT, syscall.SIGTERM)

	sig := <-quit
	logger.Log.Info("shutting down server", zap.String("signal", sig.String()))

	return s.Shutdown()
}

// Shutdown gracefully shuts down the server and closes database connection
func (s *Server) Shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Log.Error("graceful shutdown failed", zap.Error(err))
		return err
	}

	sqlDB, err := s.database.DB()
	if err == nil {
		sqlDB.Close()
	}

	logger.Log.Info("server exited cleanly")
	return nil
}
