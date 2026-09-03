package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type Server struct {
	service *service.Service
	logger  *slog.Logger
	mux     *http.ServeMux
}

func NewServer(svc *service.Service, logger *slog.Logger) *Server {
	s := &Server{service: svc, logger: logger, mux: http.NewServeMux()}

	s.mux.HandleFunc("POST /transfers", s.createTransfer)
	s.mux.HandleFunc("GET /transfers/{id}", s.getTransfer)
	s.mux.HandleFunc("POST /wallets", s.createWallet)
	s.mux.HandleFunc("GET /wallets/{id}", s.getWallet)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
