package service_test

import (
	"context"
	"testing"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository/memory"
	"Wallet_Transfer_Service/internal/service"
)

// The ledger is the source of truth: every processed transfer must leave
// behind a balanced DEBIT/CREDIT pair, and a wallet's stored balance must
// always be reconstructable from its ledger entries alone. This test runs
// a mix of successful and failing transfers across three wallets and
// checks both invariants hold globally, not just for a single transfer.
func TestLedger_BalancesReconcileAcrossManyTransfers(t *testing.T) {
	store := memory.NewStore()
	ts := service.NewTransferService(store)
	ws := service.NewWalletService(store)
	ctx := context.Background()

	mustCreateWallet(t, ws, "wallet_a", 1000)
	mustCreateWallet(t, ws, "wallet_b", 500)
	mustCreateWallet(t, ws, "wallet_c", 0)

	transfers := []service.CreateTransferRequest{
		{IdempotencyKey: "t1", FromWalletID: "wallet_a", ToWalletID: "wallet_b", Amount: 200},
		{IdempotencyKey: "t2", FromWalletID: "wallet_b", ToWalletID: "wallet_c", Amount: 300},
		{IdempotencyKey: "t3", FromWalletID: "wallet_c", ToWalletID: "wallet_a", Amount: 1_000_000}, // must fail: insufficient funds
		{IdempotencyKey: "t4", FromWalletID: "wallet_a", ToWalletID: "wallet_c", Amount: 50},
		{IdempotencyKey: "t5", FromWalletID: "wallet_c", ToWalletID: "wallet_b", Amount: 25},
	}

	var transferIDs []string
	for _, req := range transfers {
		res, _, err := ts.CreateTransfer(ctx, req)
		if err != nil {
			t.Fatalf("CreateTransfer(%s): %v", req.IdempotencyKey, err)
		}
		transferIDs = append(transferIDs, res.TransferID)
	}

	walletIDs := []string{"wallet_a", "wallet_b", "wallet_c"}
	ledgerBalance := map[string]int64{"wallet_a": 1000, "wallet_b": 500, "wallet_c": 0}
	var totalDebits, totalCredits int64

	for _, tid := range transferIDs {
		detail, err := ts.GetTransfer(ctx, tid)
		if err != nil {
			t.Fatalf("GetTransfer(%s): %v", tid, err)
		}
		for _, e := range detail.Entries {
			switch e.EntryType {
			case domain.EntryDebit:
				ledgerBalance[e.WalletID] -= e.Amount
				totalDebits += e.Amount
			case domain.EntryCredit:
				ledgerBalance[e.WalletID] += e.Amount
				totalCredits += e.Amount
			default:
				t.Fatalf("unexpected entry type %s", e.EntryType)
			}
		}
	}

	if totalDebits != totalCredits {
		t.Fatalf("ledger does not balance globally: debits=%d credits=%d", totalDebits, totalCredits)
	}

	for _, id := range walletIDs {
		w, err := ws.GetWallet(ctx, id)
		if err != nil {
			t.Fatalf("GetWallet(%s): %v", id, err)
		}
		if w.Balance != ledgerBalance[id] {
			t.Fatalf("wallet %s balance %d does not reconcile with ledger-derived balance %d", id, w.Balance, ledgerBalance[id])
		}
	}

	// t3 was designed to fail - confirm it left no ledger trace.
	failedDetail, err := ts.GetTransfer(ctx, transferIDs[2])
	if err != nil {
		t.Fatalf("GetTransfer(t3): %v", err)
	}
	if failedDetail.Transfer.State != domain.TransferFailed {
		t.Fatalf("expected t3 to be FAILED, got %s", failedDetail.Transfer.State)
	}
	if len(failedDetail.Entries) != 0 {
		t.Fatalf("FAILED transfer must have no ledger entries, got %d", len(failedDetail.Entries))
	}
}

// Every ledger entry must carry the transfer's exact amount and be tagged
// to the correct wallet and side - a DEBIT against the source, a CREDIT
// against the destination, never swapped or split.
func TestLedger_EntriesAttributeCorrectWalletAndAmount(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "src", 300)
	mustCreateWallet(t, ws, "dst", 0)

	res, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "attribution",
		FromWalletID:   "src",
		ToWalletID:     "dst",
		Amount:         77,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}

	detail, err := ts.GetTransfer(ctx, res.TransferID)
	if err != nil {
		t.Fatalf("GetTransfer: %v", err)
	}
	if len(detail.Entries) != 2 {
		t.Fatalf("expected exactly 2 entries, got %d", len(detail.Entries))
	}
	for _, e := range detail.Entries {
		if e.Amount != 77 {
			t.Fatalf("entry amount mismatch: got %d, want 77", e.Amount)
		}
		if e.TransferID != res.TransferID {
			t.Fatalf("entry not attributed to correct transfer: got %s, want %s", e.TransferID, res.TransferID)
		}
		switch e.EntryType {
		case domain.EntryDebit:
			if e.WalletID != "src" {
				t.Fatalf("DEBIT entry attributed to wrong wallet: got %s, want src", e.WalletID)
			}
		case domain.EntryCredit:
			if e.WalletID != "dst" {
				t.Fatalf("CREDIT entry attributed to wrong wallet: got %s, want dst", e.WalletID)
			}
		default:
			t.Fatalf("unexpected entry type %s", e.EntryType)
		}
	}
}
