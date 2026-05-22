package idempotency

// GetIdempotencyControllerTestCases returns the scenarios for the Idempotency Controller.
func GetIdempotencyControllerTestCases() []IdempotencyControllerTestCase {
	return []IdempotencyControllerTestCase{
		{
			Name:      "TC_IDEM_C_1: Successful ID Generation and Cache Set",
			MockSetup: func(m *IdempotencyControllerMocks) {},
			ExpectErr: false,
		},
	}
}
