package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"wallet-transfer-assignment/db"
	"wallet-transfer-assignment/model"

	"github.com/google/uuid"
)

func markFailed(tx *sql.Tx,id string, reason string, idem_key string) (model.TransferResponse, error) {
	tx.Rollback()
    _,_ = db.DB.Exec(`
			UPDATE transfers 
			SET status='FAILED', failure_reason=$2 
			WHERE id=$1
		`, id, reason)
    _,_ = db.DB.Exec(`
			UPDATE idempotency_records 
			SET transfer_id=$1
			WHERE idempotency_key=$2
		`, id, idem_key)
	return model.TransferResponse{
		TransferID: id,
		Status: "FAILED",
	}, nil
}

func ProcessTransfer(ctx context.Context, req model.TransferRequest) (model.TransferResponse, error) {

	if req.Amount <= 0 {
		return model.TransferResponse{}, errors.New("invalid amount")
	}
	
	if req.FromWalletID == req.ToWalletID {
		return model.TransferResponse{}, errors.New("cannot transfer to same wallet")
	}
	
	if req.IdempotencyKey == "" {
		return model.TransferResponse{}, errors.New("idempotency key required")
	}

	rows, err := db.DB.Query(`
		SELECT id FROM wallets WHERE id IN ($1, $2)
	`, req.FromWalletID, req.ToWalletID)
	if err != nil {
		return model.TransferResponse{}, errors.New("internal server error")
	}
	defer rows.Close()

	found := map[string]bool{}

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return model.TransferResponse{}, errors.New("internal server error")
		}
		found[id] = true
	}

	// check wallet 
	if !found[req.FromWalletID] {
		return model.TransferResponse{}, errors.New("from_wallet_not_found")
	}
	if !found[req.ToWalletID] {
		return model.TransferResponse{}, errors.New("to_wallet_not_found")
	}

	//Check idempotency
	var existing []byte
	var extTransferID sql.NullString

	err = db.DB.QueryRow(`
		INSERT INTO idempotency_records (idempotency_key)
		VALUES ($1)
		ON CONFLICT (idempotency_key) DO UPDATE 
		SET idempotency_key = EXCLUDED.idempotency_key
		RETURNING response, transfer_id
	`, req.IdempotencyKey).Scan(&existing, &extTransferID)

	if err != nil {
		return model.TransferResponse{}, err
	}

	if existing != nil {
		var resp model.TransferResponse
		json.Unmarshal(existing, &resp)
		return resp, nil
	}

	if extTransferID.Valid{
		var status string
		db.DB.QueryRow(`
			SELECT status FROM transfers WHERE id=$1
		`, extTransferID.String).Scan(&status)

		if status == "PENDING" {
			return model.TransferResponse{
				TransferID: extTransferID.String,
				Status: "PENDING",
			}, nil
		}

		if status == "FAILED" {
			return model.TransferResponse{
				TransferID: extTransferID.String,
				Status: "FAILED",
			}, nil
		}
	}


	// insert transfer
	transferID := uuid.New().String()

	_, err = db.DB.Exec(`
		INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1,$2,$3,$4,'PENDING')
	`, transferID, req.FromWalletID, req.ToWalletID, req.Amount)

	if err != nil {
		return model.TransferResponse{}, err
	}

	//   Begin TX
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return model.TransferResponse{}, err
	}
	defer tx.Rollback()

	

	//Lock wallets
	firstWalletID, secondWalletID := req.FromWalletID, req.ToWalletID

	if firstWalletID > secondWalletID {
		firstWalletID, secondWalletID = secondWalletID, firstWalletID
	}

	var firstBalance int64
	err = tx.QueryRow(
		`SELECT balance FROM wallets WHERE id=$1 FOR UPDATE`,
		firstWalletID,
	).Scan(&firstBalance)
	
	if err == sql.ErrNoRows {
		return markFailed(tx, transferID,"wallet_not_found", req.IdempotencyKey)
	}
	if err != nil {
		return markFailed(tx, transferID,"first_wallet_fetch", req.IdempotencyKey)
	}

	var secondBalance int64
	err = tx.QueryRow(
		`SELECT balance FROM wallets WHERE id=$1 FOR UPDATE`,
		secondWalletID,
	).Scan(&secondBalance)

	if err == sql.ErrNoRows {
		return markFailed(tx, transferID,"wallet_not_found", req.IdempotencyKey)
	}
	if err != nil {
		return markFailed(tx, transferID,"second_wallet_fetch", req.IdempotencyKey)
	}

	fromBalance, toBalance := firstBalance, secondBalance
	
	if firstWalletID != req.FromWalletID{
		fromBalance, toBalance = toBalance, fromBalance
	}

	//Check balance
	if fromBalance < req.Amount {
		return markFailed(tx, transferID,"insufficient_balance", req.IdempotencyKey)
	}

	//  update balances
	_, err = tx.Exec(`UPDATE wallets SET balance=balance-$1 WHERE id=$2`,
		req.Amount, req.FromWalletID)
	if err != nil {
		return markFailed(tx, transferID,"update_balance_fromWallet", req.IdempotencyKey)
	}

	_, err = tx.Exec(`UPDATE wallets SET balance=balance+$1 WHERE id=$2`,
		req.Amount, req.ToWalletID)
	if err != nil {
		return markFailed(tx, transferID,"update_balance_toWallet", req.IdempotencyKey)
	}

	//  Ledger entries
	_, err = tx.Exec(`
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES ($1,$2,'DEBIT',$3), ($4,$2,'CREDIT',$3)
	`, req.FromWalletID, transferID, req.Amount, req.ToWalletID)
	if err != nil {
		return markFailed(tx, transferID,"ledger_entries", req.IdempotencyKey)
	}

	// Update transfer
	_, err = tx.Exec("UPDATE transfers SET status='PROCESSED' WHERE id=$1", transferID)
	if err != nil {
		return markFailed(tx, transferID,"transfer_entry_update", req.IdempotencyKey)
	}

	resp := model.TransferResponse{
		TransferID: transferID,
		Status:     "PROCESSED",
	}

	// Store idempotency
	respJSON, _ := json.Marshal(resp)

	_, err = tx.Exec(`
		UPDATE idempotency_records 
		SET transfer_id=$2, response=$3
		WHERE idempotency_key=$1
	`, req.IdempotencyKey, transferID, respJSON)
	if err != nil {
		return markFailed(tx, transferID,"idempotency_entry_update", req.IdempotencyKey)
	}

	// Commit tx
	err = tx.Commit()
	if err != nil {
		return model.TransferResponse{}, err
	}

	return resp, nil
}
