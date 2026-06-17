package transfer

import (
	"context"
	"database/sql"
	"errors"
	"wallet-transfer/idempotency"
	"wallet-transfer/ledger"
	"wallet-transfer/wallet"

	"gorm.io/gorm"
)

type TransferService struct {
	Db      *gorm.DB
	Request *Transfer
}

func (ser *TransferService) TransferMoney() (interface{}, error, int) {

	// check idempotency
	idemSer := &idempotency.IdemService{Db: ser.Db}
	idemObj, idempotencyFounded := idemSer.GetIdempotencyById(ser.Request.IdempotencyKey)
	// fmt.Println("1 ", idemObj, idempotencyFounded)
	if idemObj != nil {
		// founded
		switch idemObj.Status {
		case "COMPLETED":
			return "COMPLETED", nil, 200
		case "IN_PROGRESS":
			return "IN_PROGRESS", nil, 200
		}
	} else {
		// record not founded
		if idempotencyFounded == gorm.ErrRecordNotFound {
			err := ser.CreateTransfer(context.Background(), *ser.Request)
			if err != nil {
				return "", err, 500
			}
			return "fund transferred successfully.", nil, 200
		}
		return "", idempotencyFounded, 500
	}
	return "", nil, 200
}

func (ser *TransferService) CreateTransfer(ctx context.Context, req Transfer) error {
	// fmt.Println("createa transactions", req)
	tx := ser.Db.Begin(&sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})

	var err error
	defer func() {
		if err != nil {
			// fmt.Println("defer called.")
			tx.Rollback()
		}
	}()

	// lock wallets of user1 and user2 for updates
	// fmt.Println("loc wallets")
	walletrepo := wallet.WalletRepo{}
	wallets, err := walletrepo.LockWallets(ctx, tx, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return err
	}

	// fmt.Println("locing successfully.")

	fromWallet := wallets[0]
	toWallet := wallets[1]

	if fromWallet.Balance < req.Amount {
		err = errors.New("in sufficient balance")
		return err
	}

	// fmt.Println("sufficient balance.")
	// deduct money from from user

	transferRepo := TransferRepo{}
	transfer, err := transferRepo.Create(tx, req)
	// fmt.Println("trans", transfer, err)
	if err != nil {
		return err
	}

	// fmt.Println("create transfer successfully.")

	// transfer from user --> update from user balance

	err = walletrepo.UpdateWallet(ctx, tx, req.FromWalletID, fromWallet.Balance-req.Amount)
	if err != nil {
		return err
	}
	// fmt.Println("from wallet deduct ssuccessfully.")
	ledgerRepo := ledger.LedgerRepo{}
	// ledgerRepo.CreateEntries()
	err = ledgerRepo.CreateEntries(tx, ledger.LedgerEntry{WalletID: req.FromWalletID, TransferID: transfer.ID, Type: "DEBIT", Amount: req.Amount})
	if err != nil {
		return err
	}

	// fmt.Println("debit ledger entry successfully.")

	err = walletrepo.UpdateWallet(ctx, tx, req.ToWalletID, toWallet.Balance+req.Amount)
	if err != nil {
		return err
	}

	// fmt.Println("to wallet add money successfully.")

	err = ledgerRepo.CreateEntries(tx, ledger.LedgerEntry{WalletID: req.ToWalletID, TransferID: transfer.ID, Type: "CREDIT", Amount: req.Amount})
	if err != nil {
		return err
	}

	// fmt.Println("credit ledger entry successfully.")

	err = transferRepo.UpdateState(tx, transfer.ID, "PROCESSED")
	if err != nil {
		return err
	}

	// fmt.Println("update transfer status successfully.")

	idemRepo := &idempotency.IdemRepository{}
	err = idemRepo.CreateIdempotencyEntry(tx, &idempotency.IdempotencyRecord{IdempotencyKey: req.IdempotencyKey, TransferID: transfer.ID, Status: "COMPLETED"})
	if err != nil {
		return err
	}

	// fmt.Println("creat idempotency successfully. entry.")
	err = tx.Commit().Error
	if err != nil {
		return err
	}

	// fmt.Println("successfullt commited.")
	return nil
}
