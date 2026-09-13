package main

import (
	"net/http"
	server "wallet-transfer-assigment/router"
	"wallet-transfer-assigment/utils"
)

func main() {
	log := utils.NewLogger()
	server := server.NewServer(log)
	log.Logs.Info("Setting up routes...")
	server.Routes()
	log.Logs.Info("Server is running on port 8080...")
	http.ListenAndServe(":8080", server.Handler())

}
