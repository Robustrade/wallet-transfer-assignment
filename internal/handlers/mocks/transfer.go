package mocks

import "github.com/gin-gonic/gin"

// MockTransferHandler is a manual mock for handlers.TransferHandler
type MockTransferHandler struct {
	CreateTransferFunc func(c *gin.Context)
}

func (m *MockTransferHandler) CreateTransfer(c *gin.Context) {
	if m.CreateTransferFunc != nil {
		m.CreateTransferFunc(c)
	}
}
