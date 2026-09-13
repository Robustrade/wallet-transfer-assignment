package router

import (
	"net/http"
	ctrl "wallet-transfer-assigment/controller"
	"wallet-transfer-assigment/utils"
)

type Server struct {
	Conroller ctrl.Controllerer
	Router    *http.ServeMux
}
type Serverer interface {
	Handler() *http.ServeMux
	Routes()
}

func NewServer(log utils.Logger) Serverer {

	http := http.NewServeMux()
	log.Logs.Info("Initializing the server...")
	return &Server{
		Router:    http,
		Conroller: ctrl.NewController(log),
	}
}
func (s *Server) Handler() *http.ServeMux {
	return s.Router
}
func (s *Server) Routes() {
	walletRoutes(s)
	transactionRoutes(s)
}

func walletRoutes(s *Server) {
	s.Router.HandleFunc("/wallets", walletHandler)
	s.Router.HandleFunc("/wallets/{id}", walletHandler)
	s.Router.HandleFunc("/wallets/{id}/statements", walletStatementHandler)
}
func transactionRoutes(s *Server) {
	s.Router.HandleFunc("/transactions", transactionHandler)
	s.Router.HandleFunc("/transactions/{id}", transactionHandler)
	s.Router.HandleFunc("/transactions/{id}/status", transactionStatusHandler)
}
