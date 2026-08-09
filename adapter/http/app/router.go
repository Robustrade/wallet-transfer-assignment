package app

import (
	"github.com/chaitanya-bhagat/wallet-transfer-assignment/adapter/http/controller"
	"github.com/go-chi/chi/v5"
)

func (app *TransferApp) LoadRoutes() *chi.Mux {
	router := chi.NewRouter()
	transferController := controller.NewTransferController(*app.deps.TransferService)
	router.Post("/transfer", transferController.CreateTransfer)

	return router
}
