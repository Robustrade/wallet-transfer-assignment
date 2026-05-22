package behavioral

import (
	"net/http"
)

// BehavioralTestCase defines the structure for our table-driven behavioral tests.
type BehavioralTestCase struct {
	ID                 string
	Name               string
	SourceWalletID     string
	DestWalletID       string
	SourceBalance      int64
	DestBalance        int64
	Amount             int64
	IdempotencyKey     string
	PreIssueKey        bool
	SeedSource         bool
	SeedDest           bool
	ExpectedStatusCode int
	ValidateBalances   bool
	ExpectedW1Balance  int64
	ExpectedW2Balance  int64
	ValidateLedger     bool
}

const (
	walletA = "550e8400-e29b-41d4-a716-446655440001"
	walletB = "550e8400-e29b-41d4-a716-446655440002"
)

func GetBehavioralTestCases() []BehavioralTestCase {
	cases := getSuccessCases()
	cases = append(cases, getValidationCases()...)
	cases = append(cases, getErrorCases()...)
	return cases
}

func getSuccessCases() []BehavioralTestCase {
	return []BehavioralTestCase{
		{
			ID:                 "TC1.1",
			Name:               "Successful Transfer",
			SourceWalletID:     walletA,
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             500,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440011",
			PreIssueKey:        true,
			SeedSource:         true,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusOK,
			ValidateBalances:   true,
			ExpectedW1Balance:  500,
			ExpectedW2Balance:  500,
			ValidateLedger:     true,
		},
		{
			ID:                 "TC1.2",
			Name:               "Boundary: Min Amount",
			SourceWalletID:     walletA,
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             100,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440012",
			PreIssueKey:        true,
			SeedSource:         true,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusOK,
			ValidateBalances:   true,
			ExpectedW1Balance:  900,
			ExpectedW2Balance:  100,
		},
	}
}

func getValidationCases() []BehavioralTestCase {
	return []BehavioralTestCase{
		{
			ID:                 "TC1.6",
			Name:               "Amount < 100",
			SourceWalletID:     walletA,
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             99,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440013",
			PreIssueKey:        true,
			SeedSource:         true,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			ID:                 "TC1.8",
			Name:               "Self-Transfer",
			SourceWalletID:     walletA,
			DestWalletID:       walletA,
			SourceBalance:      1000,
			DestBalance:        1000,
			Amount:             500,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440014",
			PreIssueKey:        true,
			SeedSource:         true,
			SeedDest:           false, // Source == Dest
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			ID:                 "TC1.4",
			Name:               "Missing Header",
			SourceWalletID:     walletA,
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             500,
			IdempotencyKey:     "", // Trigger missing header
			PreIssueKey:        false,
			SeedSource:         true,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			ID:                 "TC1.5",
			Name:               "Unissued Key",
			SourceWalletID:     walletA,
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             500,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440999", // Valid UUID but not issued
			PreIssueKey:        false,
			SeedSource:         true,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusBadRequest,
		},
		{
			ID:                 "TC1.11",
			Name:               "Invalid UUID Format",
			SourceWalletID:     "not-a-uuid",
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             500,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440018",
			PreIssueKey:        true,
			SeedSource:         false,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusBadRequest,
		},
	}
}

func getErrorCases() []BehavioralTestCase {
	return []BehavioralTestCase{
		{
			ID:                 "TC2.1",
			Name:               "Insufficient Funds",
			SourceWalletID:     walletA,
			DestWalletID:       walletB,
			SourceBalance:      50,
			DestBalance:        0,
			Amount:             100,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440015",
			PreIssueKey:        true,
			SeedSource:         true,
			SeedDest:           true,
			ExpectedStatusCode: http.StatusUnprocessableEntity,
			ValidateBalances:   true,
			ExpectedW1Balance:  50,
			ExpectedW2Balance:  0,
		},
		{
			ID:                 "TC1.9",
			Name:               "Non-Existent Wallet",
			SourceWalletID:     "550e8400-e29b-41d4-a716-446655440888", // Valid UUID but not seeded
			DestWalletID:       walletB,
			SourceBalance:      1000,
			DestBalance:        0,
			Amount:             500,
			IdempotencyKey:     "550e8400-e29b-41d4-a716-446655440019",
			PreIssueKey:        true,
			SeedSource:         false, // Trigger not found
			SeedDest:           true,
			ExpectedStatusCode: http.StatusBadRequest,
		},
	}
}
