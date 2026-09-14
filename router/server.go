package router

import (
	"net/http"
	ctrl "wallet-transfer-assigment/controller"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

type Server struct {
	Conrollerer ctrl.Controllerer
	Router      *http.ServeMux
	logger      utils.Logger
}
type Serverer interface {
	Handler() http.Handler
	Routes()
	walletHandler
	transactionHandler
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		s.Router.ServeHTTP(w, r)
	})
}
func NewServer(log utils.Logger) (Serverer, error) {

	router := http.NewServeMux()
	log.Info("Initializing the server...")
	controllerer, err := ctrl.NewController(log)
	if err != nil {
		log.Error(" failed to initialise controller", zap.Error(err))
		return nil, err
	}

	return &Server{
		Router:      router,
		Conrollerer: controllerer,
		logger:      log,
	}, nil
}

func (s *Server) Routes() {
	s.logger.Info("Initializing the routes")
	walletRoutes(s)
	transactionRoutes(s)
	s.logger.Info("routes are initialised")
}

func walletRoutes(s *Server) {
	s.Router.HandleFunc("/wallets", s.wallets)
	s.Router.HandleFunc("/wallets/{id}", s.getWalletById)
	s.Router.HandleFunc("/wallets/{id}/statements", s.walletStatementByWalletId)
}
func transactionRoutes(s *Server) {
	s.Router.HandleFunc("/transactions", s.getTransactions)
	s.Router.HandleFunc("/transfers", s.transfer)
}
