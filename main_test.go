package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
	database "wallet-transfer/db"
	"wallet-transfer/transfer"

	"github.com/go-playground/assert/v2"
	"github.com/google/uuid"
)

func TestTransferredMoney(t *testing.T) {
	const (
		totalRequests = 5
	)

	dbs, err := database.Connection()
	// fmt.Println(dbs, err)
	if err != nil {
		return
	}

	var wg sync.WaitGroup

	// start := time.Now()

	// mtx := &sync.Mutex{}
	// cnt := 0

	for i := 0; i < totalRequests; i++ {
		time.Sleep(1 * time.Millisecond)
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			fromWallet := fmt.Sprintf("wallet_%d", (i%5)+1)
			toWallet := fmt.Sprintf("wallet_%d", ((i+1)%5)+1)

			reqBody := transfer.Transfer{
				IdempotencyKey: uuid.New().String(),
				FromWalletID:   fromWallet,
				ToWalletID:     toWallet,
				Amount:         2,
			}

			transRepo := transfer.TransferService{Db: dbs, Request: &reqBody}
			_, _, status := transRepo.TransferMoney()
			// if status != 200 {
			// 	mtx.Lock()
			// 	cnt++
			// 	mtx.Unlock()
			// }
			assert.Equal(t, 201, status)
		}(i)
	}

	wg.Wait()

	// fmt.Printf("\nCompleted %d requests in %v\n",
	// 	totalRequests,
	// 	time.Since(start),
	// )
	// fmt.Println("successfull ", totalRequests-cnt, " failed ", cnt)
}
