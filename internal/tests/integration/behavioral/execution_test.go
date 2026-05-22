package behavioral

import (
	"fmt"
	"net/http"
	"testing"

	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/tests/integration/shared"

	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	cleanup, err := shared.SetupTestEnvironment()
	if err != nil {
		panic(err)
	}
	defer cleanup()

	m.Run()
}

func TestBehavioral_TableDriven(t *testing.T) {
	testCases := GetBehavioralTestCases()

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s: %s", tc.ID, tc.Name), func(t *testing.T) {
			shared.ClearDatabase()
			if tc.SeedSource {
				shared.SeedWallet(tc.SourceWalletID, tc.SourceBalance)
			}
			if tc.SeedDest {
				shared.SeedWallet(tc.DestWalletID, tc.DestBalance)
			}
			if tc.PreIssueKey {
				shared.PreIssueKey(tc.IdempotencyKey)
			}

			payload := map[string]interface{}{
				"fromWalletId": tc.SourceWalletID,
				"toWalletId":   tc.DestWalletID,
				"amount":       tc.Amount,
			}

			resp := shared.ExecuteTransferRequest(payload, tc.IdempotencyKey)

			assert.Equal(t, tc.ExpectedStatusCode, resp.Code)

			if tc.ValidateBalances {
				var w1, w2 models.Wallet
				shared.TestDB.First(&w1, "id = ?", tc.SourceWalletID)
				shared.TestDB.First(&w2, "id = ?", tc.DestWalletID)
				assert.Equal(t, tc.ExpectedW1Balance, w1.Balance)
				assert.Equal(t, tc.ExpectedW2Balance, w2.Balance)
			}

			if tc.ValidateLedger {
				var ledgers []models.Ledger
				shared.TestDB.Find(&ledgers)
				assert.Len(t, ledgers, 2)
			}
		})
	}
}

func TestBehavioral_Idempotency_Sequential(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	w2 := "550e8400-e29b-41d4-a716-446655440002"
	shared.SeedWallet(w1, 1000)
	shared.SeedWallet(w2, 0)
	key := "550e8400-e29b-41d4-a716-446655440021"
	shared.PreIssueKey(key)

	payload := map[string]interface{}{
		"fromWalletId": w1,
		"toWalletId":   w2,
		"amount":       100,
	}

	// First hit
	resp1 := shared.ExecuteTransferRequest(payload, key)
	assert.Equal(t, http.StatusOK, resp1.Code)

	// Second hit
	resp2 := shared.ExecuteTransferRequest(payload, key)
	assert.Equal(t, http.StatusOK, resp2.Code)
	assert.Equal(t, resp1.Body.String(), resp2.Body.String())

	// Verify one deduction only
	var wallet models.Wallet
	shared.TestDB.First(&wallet, "id = ?", w1)
	assert.Equal(t, int64(900), wallet.Balance)
}

func TestBehavioral_Idempotency_HitOnFailed(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	w2 := "550e8400-e29b-41d4-a716-446655440002"
	shared.SeedWallet(w1, 50) // Insufficient for 100
	shared.SeedWallet(w2, 0)
	key := "550e8400-e29b-41d4-a716-446655440023"
	shared.PreIssueKey(key)

	payload := map[string]interface{}{
		"fromWalletId": w1,
		"toWalletId":   w2,
		"amount":       100,
	}

	// First hit: Should fail with 422
	resp1 := shared.ExecuteTransferRequest(payload, key)
	assert.Equal(t, http.StatusUnprocessableEntity, resp1.Code)

	// Second hit: Should return CACHED 422
	resp2 := shared.ExecuteTransferRequest(payload, key)
	assert.Equal(t, http.StatusUnprocessableEntity, resp2.Code)
	assert.Equal(t, resp1.Body.String(), resp2.Body.String())
}

func TestBehavioral_Idempotency_ErrorRollback(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	w2 := "550e8400-e29b-41d4-a716-446655440002"
	shared.SeedWallet(w1, 1000)
	shared.SeedWallet(w2, 0)
	key := "550e8400-e29b-41d4-a716-446655440024"
	shared.PreIssueKey(key)

	t.Run("TC3.4: 500 Error should rollback key to ISSUED", func(t *testing.T) {
		shared.ClearDatabase()
		shared.SeedWallet(w1, 1000)
		shared.SeedWallet(w2, 0)
		shared.PreIssueKey(key)

		// Simulate DB error by closing the connection
		sqlDB, _ := shared.TestDB.DB()
		sqlDB.Close()

		payload := map[string]interface{}{
			"fromWalletId": w1,
			"toWalletId":   w2,
			"amount":       100,
		}

		resp := shared.ExecuteTransferRequest(payload, key)
		assert.Equal(t, http.StatusInternalServerError, resp.Code)

		// Verify key is still usable (rolled back to ISSUED)
		item, ok := shared.TestCache.Get(key)
		assert.True(t, ok)
		assert.Equal(t, "ISSUED", string(item.State))

		// RE-INIT DB for subsequent tests
		shared.ReinitDB()
	})
}

func TestBehavioral_Ledger_Immutability(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	w2 := "550e8400-e29b-41d4-a716-446655440002"
	shared.SeedWallet(w1, 1000)
	shared.SeedWallet(w2, 0)
	key := "550e8400-e29b-41d4-a716-446655440022"
	shared.PreIssueKey(key)

	payload := map[string]interface{}{
		"fromWalletId": w1,
		"toWalletId":   w2,
		"amount":       100,
	}
	shared.ExecuteTransferRequest(payload, key)

	// Try to update ledger
	err := shared.TestDB.Exec("UPDATE ledgers SET amount = 999").Error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Ledger entries are immutable")

	// Try to delete ledger
	err = shared.TestDB.Exec("DELETE FROM ledgers").Error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Ledger entries are immutable")
}

func TestBehavioral_GlobalBalanceSum(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	w2 := "550e8400-e29b-41d4-a716-446655440002"
	w3 := "550e8400-e29b-41d4-a716-446655440003"
	shared.SeedWallet(w1, 1000)
	shared.SeedWallet(w2, 1000)
	shared.SeedWallet(w3, 1000)

	transfers := []struct {
		from, to string
		amount   int64
		key      string
	}{
		{w1, w2, 100, "550e8400-e29b-41d4-a716-446655440031"},
		{w2, w3, 200, "550e8400-e29b-41d4-a716-446655440032"},
		{w3, w1, 300, "550e8400-e29b-41d4-a716-446655440033"},
	}

	for _, tr := range transfers {
		shared.PreIssueKey(tr.key)
		payload := map[string]interface{}{
			"fromWalletId": tr.from,
			"toWalletId":   tr.to,
			"amount":       tr.amount,
		}
		shared.ExecuteTransferRequest(payload, tr.key)
	}

	// Calculate SUM(DEBIT) and SUM(CREDIT) from ledgers
	var debitSum, creditSum int64
	shared.TestDB.Model(&models.Ledger{}).Where("type = ?", "DEBIT").Select("sum(amount)").Scan(&debitSum)
	shared.TestDB.Model(&models.Ledger{}).Where("type = ?", "CREDIT").Select("sum(amount)").Scan(&creditSum)

	assert.Equal(t, int64(600), debitSum)
	assert.Equal(t, int64(600), creditSum)
	assert.Equal(t, debitSum, creditSum, "Total Debits must equal Total Credits")
}

func TestBehavioral_DB_Constraint_Bypass(t *testing.T) {
	shared.ClearDatabase()
	w1 := "550e8400-e29b-41d4-a716-446655440001"
	shared.SeedWallet(w1, 100)

	// Attempt to bypass logic and force negative balance via direct SQL
	err := shared.TestDB.Exec("UPDATE wallets SET balance = -100 WHERE id = ?", w1).Error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "violates check constraint")
}
