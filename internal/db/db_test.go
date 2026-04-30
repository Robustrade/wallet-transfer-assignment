package db

import "testing"

func TestNewDBInvalidDSN(t *testing.T) {
	_, err := NewDB("not-a-valid-postgres-dsn")
	if err == nil {
		t.Fatalf("expected invalid dsn to return error")
	}
}
