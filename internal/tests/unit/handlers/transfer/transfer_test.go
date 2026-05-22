package transfer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/controller/mocks"
	handlers "wallet-transfer-assignment/internal/handlers/transfer"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupHandler(mocks *TransferMocks) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()

	handler := handlers.NewTransferHandler(mocks.Controller)
	r.POST("/transfers", handler.CreateTransfer)

	return r
}

func executeTestRequest(r *gin.Engine, body string, idempotencyKey string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/transfers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestTransferHandler(t *testing.T) {
	testCases := GetTransferTestCases()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Arrange
			mockController := &mocks.MockTransferController{}
			mockContainer := &TransferMocks{
				Controller: mockController,
			}
			tc.MockSetup(mockContainer)

			router := setupHandler(mockContainer)

			// Act
			response := executeTestRequest(router, tc.RequestBody, tc.IdempotencyKey)

			// Assert
			assert.Equal(t, tc.ExpectedStatusCode, response.Code)
			if tc.ExpectedBodyMatch != "" {
				assert.True(t, strings.Contains(response.Body.String(), tc.ExpectedBodyMatch),
					"Response body should contain expected match: %s", tc.ExpectedBodyMatch)
			}
		})
	}
}
