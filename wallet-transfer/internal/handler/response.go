package handler

import (
	"github.com/gin-gonic/gin"
)

// Response struct defines the standard JSON response format
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// Success is a helper to format successful API responses
func RequestSuccess(c *gin.Context, statusCode int, data interface{}) {
	c.JSON(statusCode, Response{
		Success: true,
		Data:    data,
	})
}

// Error is a helper to format failed API responses
func RequestError(c *gin.Context, statusCode int, message string) {
	c.JSON(statusCode, Response{
		Success: false,
		Error:   message,
	})
}

// RequestErrorWithData handles edge cases (like 422s) where you need to return
// a failure status but still provide the client with the cached entity data.
func RequestErrorWithData(c *gin.Context, statusCode int, message string, data interface{}) {
	c.JSON(statusCode, Response{
		Success: false,
		Error:   message,
		Data:    data,
	})
}
