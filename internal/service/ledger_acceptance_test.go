package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

func TestLedger_LargeTurnoverReconcilesWithoutInt64AggregateOverflow(t *testing.T) {
	f := setup(t)
	amount := int64(math.MaxInt64 / 2)
	f.exec("UPDATE wallets SET opening_balance=?,balance=? WHERE id='wallet_1'", amount, amount)
	f.exec("UPDATE wallets SET opening_balance=0,balance=0 WHERE id='wallet_2'")
	svc := service.NewTransferService(f.db, 5*time.Second, math.MaxInt64)
	for i := range 4 {
		request := transferRequest(fmt.Sprintf("large-%d", i), amount)
		if i%2 == 1 {
			request.FromWalletID, request.ToWalletID = request.ToWalletID, request.FromWalletID
		}
		if result, err := svc.CreateTransfer(f.ctx, request); err != nil || result.StatusCode != 201 {
			t.Fatalf("large transfer: %+v, %v", result, err)
		}
	}
	// Total credits exceed int64, while each individual wallet balance fits.
	f.wantBalance("wallet_1", amount)
	f.wantBalance("wallet_2", 0)
	f.wantCount("ledger_entries", 8)
	f.wantReconciled()
}

func TestLedger_RecordedHistoryRejectsEditsAndDeletes(t *testing.T) {
	f := setup(t)
	f.mustCreate("k1", 100)
	for _, query := range []string{"UPDATE ledger_entries SET amount=1", "DELETE FROM ledger_entries"} {
		err := f.db.WithContext(f.ctx).Exec(query).Error
		wantConstraintViolation(t, err)
	}
	f.wantCount("ledger_entries", 2)
	f.wantReconciled()
}

func TestLedger_DatabaseRejectsNegativeStoredBalance(t *testing.T) {
	f := setup(t)
	err := f.db.WithContext(f.ctx).Exec("UPDATE wallets SET balance=-1 WHERE id='wallet_1'").Error
	wantConstraintViolation(t, err)
	f.wantBalance("wallet_1", 1000)
	f.wantReconciled()
}

func TestLedger_IncompleteProcessedPairCannotCommit(t *testing.T) {
	f := setup(t)
	id := f.pending("k1", 100, "wallet_1", "wallet_2")
	tx := f.db.WithContext(f.ctx).Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	if err := tx.Exec("INSERT INTO ledger_entries(transfer_id,wallet_id,type,amount) VALUES(?,'wallet_1','DEBIT',100)", id).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE transfers SET status='PROCESSED' WHERE id=?", id).Error; err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(service.TransferResponse{TransferID: id, Status: domain.Processed})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("UPDATE idempotency_records SET response_status=201,response_body=? WHERE transfer_id=?", string(body), id).Error; err != nil {
		t.Fatal(err)
	}
	wantConstraintViolation(t, tx.Commit().Error)
	f.wantPending(id)
	f.wantCount("ledger_entries", 0)
	f.wantReconciled()
}

func wantConstraintViolation(t *testing.T, err error) {
	t.Helper()
	var pgError *pgconn.PgError
	if !errors.As(err, &pgError) || pgError.Code != "23514" {
		t.Fatalf("want check/trigger rejection (23514), got %v", err)
	}
}
