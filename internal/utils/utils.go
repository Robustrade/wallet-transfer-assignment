package utils

import (
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ErrorResponse(msg string) gin.H {
	return gin.H{"error": msg}
}

func ParseUint(c *gin.Context, param string) (uint64, error) {
	v, err := strconv.ParseUint(c.Param(param), 10, 64)
	if err != nil {
		c.JSON(400, ErrorResponse("invalid "+param))
	}
	return v, err
}

func QueryInt(c *gin.Context, key string, def int) int {
	v, err := strconv.Atoi(c.Query(key))
	if err != nil || v < 0 {
		return def
	}
	return v
}

func GetEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
