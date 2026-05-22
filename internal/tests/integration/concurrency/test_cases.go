package concurrency

type ConcurrencyTestCase struct {
	ID             string
	Name           string
	SourceWalletID string
	DestWalletID   string
	InitialBalance int64
	NumRoutines    int
	AmountPerTx    int64
	ExpectedStatus map[int]int // map[http_status_code]count
	FinalW1Balance int64
	FinalW2Balance int64
}

const (
	multiSource = "MULTI_SOURCE"
	parallel    = "PARALLEL"
	deadlock    = "DEADLOCK"
)

func GetConcurrencyTestCases() []ConcurrencyTestCase {
	walletA := "550e8400-e29b-41d4-a716-446655440003"
	walletB := "550e8400-e29b-41d4-a716-446655440004"

	return []ConcurrencyTestCase{
		{
			ID:             "TC4.2",
			Name:           "High Contention: Source Wallet Exhaustion",
			SourceWalletID: walletA,
			DestWalletID:   walletB,
			InitialBalance: 1000,
			NumRoutines:    15,
			AmountPerTx:    100,
			ExpectedStatus: map[int]int{
				200: 10,
				422: 5, // Insufficient funds
			},
			FinalW1Balance: 0,
			FinalW2Balance: 1000,
		},
		{
			ID:             "TC4.3",
			Name:           "High Contention: Dest Wallet",
			SourceWalletID: multiSource, // special flag for runner
			DestWalletID:   walletB,
			InitialBalance: 1000,
			NumRoutines:    10,
			AmountPerTx:    100,
			ExpectedStatus: map[int]int{
				200: 10,
			},
			FinalW2Balance: 1000,
		},
		{
			ID:             "TC4.4",
			Name:           "Parallel Execution",
			SourceWalletID: parallel, // special flag
			InitialBalance: 1000,
			NumRoutines:    2,
			AmountPerTx:    100,
			ExpectedStatus: map[int]int{
				200: 2,
			},
		},
		{
			ID:             "TC4.1",
			Name:           "Deadlock Prevention",
			SourceWalletID: deadlock, // special flag
			InitialBalance: 1000,
			NumRoutines:    2,
			AmountPerTx:    100,
			ExpectedStatus: map[int]int{
				200: 2,
			},
		},
	}
}
