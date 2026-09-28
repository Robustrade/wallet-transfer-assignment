package integration

import (
	"math/big"
)

// checkInvariants verifies the money-safety properties that must hold after
// ANY sequence of operations. newEnv registers it as a cleanup, so it runs
// after every integration test.
//
// Sums use math/big because overflow tests seed balances near MaxInt64,
// where an int64 (or SQL SUM) total would itself overflow.
func (e *testEnv) checkInvariants() {
	t := e.t
	t.Helper()

	type transfer struct {
		from, to, status string
		amount           int64
	}
	type entry struct {
		walletID, entryType string
		amount              int64
	}

	// Wallet balances.
	balances := map[string]int64{}
	if !e.eachRow("SELECT id, balance FROM wallets", func(scan func(...any) error) error {
		var id string
		var bal int64
		err := scan(&id, &bal)
		balances[id] = bal
		return err
	}) {
		return
	}

	// Transfers.
	transfers := map[string]transfer{}
	if !e.eachRow("SELECT id, from_wallet_id, to_wallet_id, amount, status FROM transfers", func(scan func(...any) error) error {
		var id string
		var tr transfer
		err := scan(&id, &tr.from, &tr.to, &tr.amount, &tr.status)
		transfers[id] = tr
		return err
	}) {
		return
	}

	// Ledger entries, grouped by transfer.
	entries := map[string][]entry{}
	if !e.eachRow("SELECT transfer_id, wallet_id, entry_type, amount FROM ledger_entries", func(scan func(...any) error) error {
		var transferID string
		var en entry
		err := scan(&transferID, &en.walletID, &en.entryType, &en.amount)
		entries[transferID] = append(entries[transferID], en)
		return err
	}) {
		return
	}

	e.mu.Lock()
	opening := make(map[string]int64, len(e.opening))
	for k, v := range e.opening {
		opening[k] = v
	}
	e.mu.Unlock()

	// 1. Money is conserved: total balance equals the seeded total.
	seededTotal, currentTotal := new(big.Int), new(big.Int)
	for _, v := range opening {
		seededTotal.Add(seededTotal, big.NewInt(v))
	}
	for _, v := range balances {
		currentTotal.Add(currentTotal, big.NewInt(v))
	}
	if seededTotal.Cmp(currentTotal) != 0 {
		t.Errorf("invariant: total balance %s != seeded total %s", currentTotal, seededTotal)
	}

	// 2-4. Ledger shape per transfer status.
	for id, tr := range transfers {
		es := entries[id]
		switch tr.status {
		case "PROCESSED":
			var debits, credits int
			for _, en := range es {
				switch {
				case en.entryType == "DEBIT" && en.walletID == tr.from && en.amount == tr.amount:
					debits++
				case en.entryType == "CREDIT" && en.walletID == tr.to && en.amount == tr.amount:
					credits++
				}
			}
			if len(es) != 2 || debits != 1 || credits != 1 {
				t.Errorf("invariant: PROCESSED transfer %s must have exactly one DEBIT(%s) and one CREDIT(%s) of %d, got %+v",
					id, tr.from, tr.to, tr.amount, es)
			}
		case "FAILED":
			if len(es) != 0 {
				t.Errorf("invariant: FAILED transfer %s has %d ledger entries, want 0", id, len(es))
			}
		case "PENDING":
			// 4. No request may finish and leave a PENDING transfer behind.
			t.Errorf("invariant: transfer %s is still PENDING", id)
			if len(es) != 0 {
				t.Errorf("invariant: PENDING transfer %s has %d ledger entries, want 0", id, len(es))
			}
		default:
			t.Errorf("invariant: transfer %s has unknown status %q", id, tr.status)
		}
	}
	for transferID := range entries {
		if _, ok := transfers[transferID]; !ok {
			t.Errorf("invariant: ledger entries reference unknown transfer %s", transferID)
		}
	}

	// 5. Each balance equals opening + credits - debits.
	expected := map[string]*big.Int{}
	for id, v := range opening {
		expected[id] = big.NewInt(v)
	}
	for _, es := range entries {
		for _, en := range es {
			exp, ok := expected[en.walletID]
			if !ok {
				exp = new(big.Int)
				expected[en.walletID] = exp
			}
			switch en.entryType {
			case "CREDIT":
				exp.Add(exp, big.NewInt(en.amount))
			case "DEBIT":
				exp.Sub(exp, big.NewInt(en.amount))
			}
		}
	}
	for id, bal := range balances {
		exp, ok := expected[id]
		if !ok {
			t.Errorf("invariant: wallet %s was not seeded by the test", id)
			continue
		}
		if exp.Cmp(big.NewInt(bal)) != 0 {
			t.Errorf("invariant: wallet %s balance %d != opening + credits - debits = %s", id, bal, exp)
		}
	}

	// 6. Every idempotency record points at the transfer it produced.
	var unlinked int64
	if err := e.db.QueryRow("SELECT COUNT(*) FROM idempotency_records WHERE transfer_id IS NULL").Scan(&unlinked); err != nil {
		t.Errorf("invariants: count unlinked idempotency records: %v", err)
	} else if unlinked != 0 {
		t.Errorf("invariant: %d idempotency records have no transfer_id", unlinked)
	}
}

// eachRow runs query and calls fn once per row with a scan function. It
// reports failures through t.Errorf and returns false if the rows could not
// be read completely.
func (e *testEnv) eachRow(query string, fn func(scan func(...any) error) error) bool {
	t := e.t
	t.Helper()
	rows, err := e.db.Query(query)
	if err != nil {
		t.Errorf("invariants: %s: %v", query, err)
		return false
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows.Scan); err != nil {
			t.Errorf("invariants: scan %s: %v", query, err)
			return false
		}
	}
	if err := rows.Err(); err != nil {
		t.Errorf("invariants: %s: %v", query, err)
		return false
	}
	return true
}
