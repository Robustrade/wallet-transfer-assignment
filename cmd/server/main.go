
package main

import (
    "log"
    "net/http"

    "wallet-transfer-assignment/internal/database"
    "wallet-transfer-assignment/internal/handler"
    "wallet-transfer-assignment/internal/repository"
    "wallet-transfer-assignment/internal/service"
)

func main() {
    db := database.NewDB()

    walletRepo := repository.NewWalletRepository(db)
    transferRepo := repository.NewTransferRepository(db)
    ledgerRepo := repository.NewLedgerRepository(db)

    transferService := service.NewTransferService(
        db,
        walletRepo,
        transferRepo,
        ledgerRepo,
    )

    mux := http.NewServeMux()

    walletHandler := handler.NewWalletHandler(walletRepo)
    transferHandler := handler.NewTransferHandler(transferService)

    mux.HandleFunc("/wallets", walletHandler.HandleWallets)
    mux.HandleFunc("/transfers", transferHandler.CreateTransfer)

    log.Println("server running on :8080")

    err := http.ListenAndServe(":8080", mux)
    if err != nil {
        log.Fatal(err)
    }
}
