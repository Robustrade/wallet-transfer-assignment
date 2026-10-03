package apitest

import (
	"maps"
	"net/http"
	"testing"
)

func TestConcurrentDebitsDoNotOverspend(t *testing.T) {
	beginTest(t)
	from := createWallet(t, 1000)
	to := createWallet(t, 0)

	// 50 transfers of 100 from a balance of 1000: exactly 10 can succeed.
	bodies := make([]string, 50)
	for i := range bodies {
		bodies[i] = transferBody(newKey(), from, to, 100)
	}
	responses := postConcurrently(t, bodies)

	statusCounts := map[int]int{}
	for _, resp := range responses {
		statusCounts[resp.status]++
	}
	want := map[int]int{http.StatusCreated: 10, http.StatusUnprocessableEntity: 40}
	if !maps.Equal(statusCounts, want) {
		t.Errorf("status counts = %v, want %v", statusCounts, want)
	}
	if balance := balanceOf(t, from); balance != 0 {
		t.Errorf("source balance = %d, want 0", balance)
	}
	if balance := balanceOf(t, to); balance != 1000 {
		t.Errorf("destination balance = %d, want 1000", balance)
	}
}

func TestOppositeTransfersDoNotDeadlock(t *testing.T) {
	beginTest(t)
	a := createWallet(t, 1000)
	b := createWallet(t, 1000)

	// 25 transfers of 10 each way: both balances stay positive throughout.
	bodies := make([]string, 50)
	for i := range bodies {
		if i%2 == 0 {
			bodies[i] = transferBody(newKey(), a, b, 10)
		} else {
			bodies[i] = transferBody(newKey(), b, a, 10)
		}
	}
	responses := postConcurrently(t, bodies)

	for i, resp := range responses {
		if resp.status != http.StatusCreated {
			t.Errorf("response %d = %d %s, want 201", i, resp.status, resp.body)
		}
	}
	if balance := balanceOf(t, a); balance != 1000 {
		t.Errorf("balance of a = %d, want 1000", balance)
	}
	if balance := balanceOf(t, b); balance != 1000 {
		t.Errorf("balance of b = %d, want 1000", balance)
	}
}
