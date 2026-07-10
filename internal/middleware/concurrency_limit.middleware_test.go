package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/loopinnovates/wallet-transfer-assignment/internal/middleware"
	"github.com/stretchr/testify/assert"
)

func TestConcurrencyLimit_AllowsUpToMax(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	limited := middleware.ConcurrencyLimit(3)(ok)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		limited.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should be allowed", i)
	}
}

func TestConcurrencyLimit_RejectsBeyondMax(t *testing.T) {
	const max = 2
	release := make(chan struct{})
	started := make(chan struct{}, max)
	var wg sync.WaitGroup

	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	limited := middleware.ConcurrencyLimit(max)(blocking)

	for i := 0; i < max; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			limited.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		}()
	}

	for range max {
		<-started
	}

	rec := httptest.NewRecorder()
	limited.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "busy")

	close(release)
	wg.Wait()
}
