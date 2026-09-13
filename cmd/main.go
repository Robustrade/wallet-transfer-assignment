package main

import (
	"net/http"
	server "wallet-transfer-assigment/router"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

var (
	Router server.Serverer
)

func main() {
	log := utils.NewLogger()
	var err error
	Router, err = server.NewServer(log)
	if err != nil {
		log.Error("failed to initialise server ", zap.Error(err))
		return
	}
	log.Info("Setting up routes...")
	Router.Routes()
	log.Info("Server is running on port 8080...")
	http.ListenAndServe(":8080", Router.Handler())

}
