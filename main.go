package main

import (
	"fmt"
	database "wallet-transfer/db"
	"wallet-transfer/transfer"

	"github.com/gin-gonic/gin"
)

func main() {
	db, err := database.Connection()

	fmt.Println(db, err)
	app := gin.Default()
	transfer.TransferController(app, db)
	app.Run(":8080")
}
