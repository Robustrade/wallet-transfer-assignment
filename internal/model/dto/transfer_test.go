package dto

import "testing"

func TestNewErrorResponse(t *testing.T) {
	got := NewErrorResponse("invalid body", "INVALID_REQUEST")

	if got.Status != "failure" {
		t.Fatalf("expected status failure, got %s", got.Status)
	}
	if got.Error.Message != "invalid body" {
		t.Fatalf("expected error message to match")
	}
	if got.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected error code to match")
	}
}
