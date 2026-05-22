package idempotency

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/controller/mocks"
	handlers "wallet-transfer-assignment/internal/handlers/idempotency"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func setupHandler(mocks *IdempotencyMocks) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()

	handler := handlers.NewIdempotencyHandler(mocks.Controller)
	r.POST("/generate-id", handler.GenerateID)

	return r
}

func executeTestRequest(r *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/generate-id", nil)
	r.ServeHTTP(w, req)
	return w
}

func TestIdempotencyHandler(t *testing.T) {
	testCases := GetIdempotencyTestCases()

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			// Arrange
			mockController := &mocks.MockIdempotencyController{}
			mockContainer := &IdempotencyMocks{
				Controller: mockController,
			}
			tc.MockSetup(mockContainer)

			router := setupHandler(mockContainer)

			// Act
			response := executeTestRequest(router)

			// Assert
			assert.Equal(t, tc.ExpectedStatusCode, response.Code)
			assert.True(t, strings.Contains(response.Body.String(), tc.ExpectedBodyMatch),
				"Response body should contain expected match: %s", tc.ExpectedBodyMatch)
		})
	}
}
