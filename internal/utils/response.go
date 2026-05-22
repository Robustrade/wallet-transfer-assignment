package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const successKey = "success"

// Exposed Method to send General Validation Error in API Response
func GeneralAPIValidationError(c *gin.Context, errorMessage string) {
	generalAPIError(c, errorMessage, http.StatusBadRequest)
}

// Exposed Method to send General Internal Error in API Response
func GeneralAPIInternalError(c *gin.Context, errorMessage string) {
	generalAPIError(c, errorMessage, http.StatusInternalServerError)
}

// Exposed Method to send General Conflict Error in API Response
func GeneralAPIConflictError(c *gin.Context, errorMessage string) {
	generalAPIError(c, errorMessage, http.StatusConflict)
}

// Exposed Method to send General Error in API Response
func generalAPIError(c *gin.Context, errorMessage string, statusCode int) {
	c.JSON(statusCode, gin.H{
		successKey:      false,
		"error_message": errorMessage,
	})
}

// Exposed method to send custom error in API Response
func CustomAPIErrorWithMeta(c *gin.Context, statusCode int, errorMessage string, errorMeta gin.H) {
	errorResponse := gin.H{
		successKey:      false,
		"error_message": errorMessage,
		"error_meta":    errorMeta,
	}

	c.JSON(statusCode, errorResponse)
}

// Exposed Method to send successful API Response
func GenerateSuccessResponse(c *gin.Context, dataResponse interface{}) {
	if dataResponse == nil {
		c.JSON(http.StatusOK, gin.H{successKey: true})
		return
	}

	// Try to convert to gin.H if it's a map to inject success
	if m, ok := dataResponse.(gin.H); ok {
		m[successKey] = true
		c.JSON(http.StatusOK, m)
		return
	}

	// For struct responses where we can't easily inject the key dynamically,
	// wrap it or rely on the struct having a Success field.
	// Since the previous implementation accepted interface{}, we wrap it if not gin.H
	c.JSON(http.StatusOK, gin.H{
		successKey: true,
		"data":     dataResponse,
	})
}
