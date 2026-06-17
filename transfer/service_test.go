package transfer

import (
	"fmt"
	"sync"
	"time"
	database "wallet-transfer/db"

	"github.com/google/uuid"
)

func run() {
	const (
		totalRequests = 1000
	)

	db, err := database.Connection()
	if err != nil {
		fmt.Println("db error", err)
		return
	}

	var wg sync.WaitGroup

	start := time.Now()

	mtx := &sync.Mutex{}
	cnt := 0

	for i := 0; i < totalRequests; i++ {
		time.Sleep(1 * time.Millisecond)
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			fromWallet := fmt.Sprintf("wallet_%d", (i%5)+1)
			toWallet := fmt.Sprintf("wallet_%d", ((i+1)%5)+1)

			reqBody := Transfer{
				IdempotencyKey: uuid.New().String(),
				FromWalletID:   fromWallet,
				ToWalletID:     toWallet,
				Amount:         2,
			}

			transRepo := TransferService{Db: db, Request: &reqBody}
			_, _, status := transRepo.TransferMoney()
			if status != 200 {
				mtx.Lock()
				cnt++
				mtx.Unlock()
			}
		}(i)
	}

	wg.Wait()

	fmt.Printf("\nCompleted %d requests in %v\n",
		totalRequests,
		time.Since(start),
	)
	fmt.Println("successfull ", totalRequests-cnt, " failed ", cnt)
}
