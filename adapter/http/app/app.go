package app

import (
	"errors"
	"log"
	"net/http"
)

type TransferApp struct {
	deps   *Dependencies
	server *http.Server
}

func NewTransferApp(dep *Dependencies) *TransferApp {
	return &TransferApp{
		deps: dep,
	}
}
func (app *TransferApp) Start() {
	app.server = &http.Server{
		Addr:    ":8080",
		Handler: app.LoadRoutes(),
	}
	log.Println("server running on :8080")
	err := app.server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}

}
