package transfer

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// var db *gorm.DB

type Controller struct {
	Db *gorm.DB
}

func TransferController(app *gin.Engine, Db *gorm.DB) {
	controller := &Controller{Db: Db}
	app.POST("/transfer", controller.transferMoney)
}

func (controller *Controller) transferMoney(ctx *gin.Context) {
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

	transferService := &TransferService{Db: controller.Db, Request: &transfer}

	res, err, status := transferService.TransferMoney()
	if err != nil {
		ctx.JSON(status, gin.H{"message": err.Error()})
		return
	}
	ctx.JSON(200, res)
}
