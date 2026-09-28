package handler_test

import (
	"os"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode) // silence Gin's debug route listing
	os.Exit(m.Run())
}
