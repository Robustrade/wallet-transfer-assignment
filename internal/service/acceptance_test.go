package service_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler"
)

func (f fixture) postParallel(requests []domain.TransferRequest) []*httptest.ResponseRecorder {
	f.t.Helper()
	router := handler.NewRouter(f.svc, testMaxTransferAmount)
	responses := make([]*httptest.ResponseRecorder, len(requests))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, request := range requests {
		httpRequest := f.httpRequest(request)
		responses[i] = httptest.NewRecorder()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			router.ServeHTTP(responses[i], httpRequest)
		}()
	}
	close(start)
	wg.Wait()
	return responses
}

func TestAcceptance_ParallelHTTPDuplicatesMoveMoneyOnce(t *testing.T) {
	f := setup(t)
	requests := make([]domain.TransferRequest, 10)
	for i := range requests {
		requests[i] = transferRequest("same-key", 100)
	}
	responses := f.postParallel(requests)
	fresh := 0
	for _, response := range responses {
		if response.Code != http.StatusCreated || response.Body.String() != responses[0].Body.String() {
			t.Fatalf("duplicate response: %d %s", response.Code, response.Body)
		}
		if response.Header().Get("Idempotent-Replayed") == "" {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh responses = %d, want 1", fresh)
	}
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
	f.wantBalance("wallet_1", 900)
	f.wantReconciled()
}

func TestAcceptance_ParallelHTTPPayloadConflictHasOneWinner(t *testing.T) {
	f := setup(t)
	responses := f.postParallel([]domain.TransferRequest{transferRequest("k1", 100), transferRequest("k1", 200)})
	counts := map[int]int{}
	for _, response := range responses {
		counts[response.Code]++
	}
	if counts[201] != 1 || counts[409] != 1 {
		t.Fatalf("statuses = %v, want one 201 and one 409", counts)
	}
	f.wantCount("transfers", 1)
	f.wantCount("ledger_entries", 2)
	f.wantReconciled()
}

func TestAcceptance_ParallelHTTPDebitsReconcileProcessedFailedAndPending(t *testing.T) {
	f := setup(t)
	pending := f.pending("abandoned", 1, "wallet_1", "wallet_2")
	requests := make([]domain.TransferRequest, 20)
	for i := range requests {
		requests[i] = transferRequest(fmt.Sprintf("key-%d", i), 100)
	}
	counts := map[int]int{}
	for _, response := range f.postParallel(requests) {
		counts[response.Code]++
	}
	if counts[201] != 10 || counts[422] != 10 {
		t.Fatalf("statuses = %v, want ten 201 and ten 422", counts)
	}
	f.wantBalance("wallet_1", 0)
	f.wantBalance("wallet_2", 1500)
	f.wantPending(pending)
	f.wantCount("transfers", 21)
	f.wantCount("ledger_entries", 20)
	f.wantReconciled()
}

func TestAcceptance_OppositeHTTPDirectionsConserveBalances(t *testing.T) {
	f := setup(t)
	requests := make([]domain.TransferRequest, 20)
	for i := range requests {
		requests[i] = transferRequest(fmt.Sprintf("key-%d", i), 10)
		if i%2 == 1 {
			requests[i].FromWalletID, requests[i].ToWalletID = requests[i].ToWalletID, requests[i].FromWalletID
		}
	}
	for _, response := range f.postParallel(requests) {
		if response.Code != 201 {
			t.Fatalf("opposite-direction response: %d %s", response.Code, response.Body)
		}
	}
	f.wantBalance("wallet_1", 1000)
	f.wantBalance("wallet_2", 500)
	f.wantReconciled()
}

// All sums use NUMERIC, including the negated debit before aggregation.
func (f fixture) wantReconciled() {
	f.t.Helper()
	var invalid int64
	err := f.db.WithContext(f.ctx).Raw(`
		SELECT count(*) FROM wallets w
		WHERE w.balance::numeric <> w.opening_balance::numeric + COALESCE((
			SELECT sum(CASE type WHEN 'CREDIT' THEN amount::numeric ELSE -amount::numeric END)
			FROM ledger_entries WHERE wallet_id=w.id
		),0)`).Row().Scan(&invalid)
	if err != nil {
		f.t.Fatal(err)
	}
	if invalid != 0 {
		f.t.Fatalf("%d wallets do not reconcile with their ledger", invalid)
	}
	err = f.db.WithContext(f.ctx).Raw(`
		SELECT count(*) FROM transfers t WHERE
		(t.status='PROCESSED' AND (
			(SELECT count(*) FROM ledger_entries WHERE transfer_id=t.id) <> 2 OR
			(SELECT count(*) FROM ledger_entries WHERE transfer_id=t.id AND amount=t.amount AND
			 ((type='DEBIT' AND wallet_id=t.from_wallet_id) OR (type='CREDIT' AND wallet_id=t.to_wallet_id))) <> 2
		)) OR (t.status<>'PROCESSED' AND EXISTS(SELECT 1 FROM ledger_entries WHERE transfer_id=t.id))
	`).Row().Scan(&invalid)
	if err != nil {
		f.t.Fatal(err)
	}
	if invalid != 0 {
		f.t.Fatalf("%d transfers have invalid ledger entries", invalid)
	}
	var balanced bool
	err = f.db.WithContext(f.ctx).Raw(`
		SELECT COALESCE(sum(CASE type WHEN 'CREDIT' THEN amount::numeric ELSE -amount::numeric END),0)=0
		FROM ledger_entries`).Row().Scan(&balanced)
	if err != nil {
		f.t.Fatal(err)
	}
	if !balanced {
		f.t.Fatal("total ledger debits and credits differ")
	}
}
