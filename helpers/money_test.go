package helpers_test

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
)

func TestMoney_Int64(t *testing.T) {
	t.Parallel()
	if helpers.Money(42).Int64() != 42 {
		t.Fatal("Int64")
	}
}
