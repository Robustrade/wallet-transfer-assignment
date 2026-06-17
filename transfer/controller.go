package transfer

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var db *gorm.DB

func TransferController(app *gin.Engine, Db *gorm.DB) {
	db = Db
	app.POST("/transfer", transferMoney)
}

func transferMoney(ctx *gin.Context) {
	var transfer Transfer

	err := ctx.ShouldBindJSON(&transfer)
	if err != nil {
		ctx.JSON(400, "bind json problem and bad request.")
		return
	}
	fmt.Println("request is ", transfer)
	if transfer.FromWalletID == transfer.ToWalletID {
		ctx.JSON(422, "Cannot transfer money to the same wallet.")
		return
	}

	trasnferService := &TransferService{Db: db, Request: &transfer}

	res, err, status := trasnferService.TransferMoney()
	if err != nil {
		ctx.JSON(status, gin.H{"message": err.Error()})
		return
	}
	ctx.JSON(200, res)
}
