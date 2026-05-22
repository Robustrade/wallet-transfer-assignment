package idempotency

import (
	"errors"
	"net/http"
)

// GetIdempotencyTestCases returns the list of test scenarios for the Idempotency Handler.
func GetIdempotencyTestCases() []IdempotencyTestCase {
	return []IdempotencyTestCase{
		{
			Name: "TC_IDEM_H_1: Successful ID Generation",
			MockSetup: func(m *IdempotencyMocks) {
				m.Controller.GenerateIDFunc = func() (string, error) {
					return "550e8400-e29b-41d4-a716-446655440000", nil
				}
			},
			ExpectedStatusCode: http.StatusOK,
			ExpectedBodyMatch:  `"id":"550e8400-e29b-41d4-a716-446655440000"`,
		},
		{
			Name: "TC_IDEM_H_2: Controller Failure Returns 500",
			MockSetup: func(m *IdempotencyMocks) {
				m.Controller.GenerateIDFunc = func() (string, error) {
					return "", errors.New("internal server error")
				}
			},
			ExpectedStatusCode: http.StatusInternalServerError,
			ExpectedBodyMatch:  `"error_message":"internal server error"`,
		},
	}
}
