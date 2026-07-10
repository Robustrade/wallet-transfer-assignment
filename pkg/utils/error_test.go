package utils_test

import (
	"testing"

	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestCustomError(t *testing.T) {
	customErr := utils.NewCustomError(500, "This is a custom error message")

	assert.Error(t, customErr)
	assert.Equal(t, 500, customErr.(*utils.CustomError).StatusCode)
	assert.Equal(t, "This is a custom error message", customErr.Error())
}
