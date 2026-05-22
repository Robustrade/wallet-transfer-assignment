package mocks

import "github.com/gin-gonic/gin"

// MockIdempotencyHandler is a manual mock for handlers.IdempotencyHandler
type MockIdempotencyHandler struct {
	GenerateIDFunc func(c *gin.Context)
}

func (m *MockIdempotencyHandler) GenerateID(c *gin.Context) {
	if m.GenerateIDFunc != nil {
		m.GenerateIDFunc(c)
	}
}
