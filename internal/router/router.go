package router

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/handler"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/middleware"
	"github.com/loopinnovates/wallet-transfer-assignment/internal/service"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/construct"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/logger"
	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
)

const defaultMaxConcurrentRequests = 200

type RouterContext struct {
	WalletSvc             service.IWalletSvc
	MaxConcurrentRequests int
}

func New(rc *RouterContext) *mux.Router {
	maxConcurrent := rc.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrentRequests
	}

	transferHandler := utils.Wrap(handler.TransferHandler(rc.WalletSvc), middleware.ConcurrencyLimit(maxConcurrent))
	walletBalanceHandler := utils.Wrap(handler.WalletBalanceHandler(rc.WalletSvc), middleware.ConcurrencyLimit(maxConcurrent))
	transferHistoryHandler := utils.Wrap(handler.TransferHistoryHandler(rc.WalletSvc), middleware.ConcurrencyLimit(maxConcurrent))

	router := mux.NewRouter()
	router.Use(middleware.Recover)
	router.HandleFunc("/health", handler.Health).Methods("GET")
	router.Handle("/transfers", transferHandler).Methods("POST")
	router.Handle("/wallets/{id}/balance", walletBalanceHandler).Methods("GET")
	router.Handle("/wallets/{id}/transfers", transferHistoryHandler).Methods("GET")

	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info().Str("method", r.Method).Str("path", r.URL.Path).Msg("route not found")
		utils.WriteError(w, construct.ErrRouteNotFound)
	})
	return router
}
