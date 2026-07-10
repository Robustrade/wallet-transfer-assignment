package utils_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/loopinnovates/wallet-transfer-assignment/pkg/utils"
	"github.com/stretchr/testify/assert"
)

func TestWriteSuccess_WithValidPayload(t *testing.T) {

	rr := httptest.NewRecorder()

	payload := map[string]string{"message": "success"}
	utils.WriteSuccess(rr, http.StatusOK, payload)

	assert.Equal(t, http.StatusOK, rr.Code)

	expectedBody := `{"message":"success"}` + "\n"
	assert.Equal(t, expectedBody, rr.Body.String())
}

func TestWriteSuccess_WithoutPayload(t *testing.T) {

	rr := httptest.NewRecorder()

	utils.WriteSuccess(rr, http.StatusOK, nil)

	assert.Equal(t, http.StatusOK, rr.Code)

	expectedBody := ""
	assert.Equal(t, expectedBody, rr.Body.String())
}

func TestWriteSuccess_CheckContentType(t *testing.T) {
	rr := httptest.NewRecorder()
	payload := map[string]string{"message": "success"}
	utils.WriteSuccess(rr, http.StatusOK, payload)

	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}

func TestWriteError(t *testing.T) {
	rr := httptest.NewRecorder()

	err := utils.NewCustomError(http.StatusInternalServerError, "an error occurred")

	utils.WriteError(rr, err)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)

	expectedBody := `{"error":"an error occurred"}` + "\n"
	assert.Equal(t, expectedBody, rr.Body.String())
}

func TestWriteError_CheckContentType(t *testing.T) {
	rr := httptest.NewRecorder()

	err := utils.NewCustomError(http.StatusInternalServerError, "an error occurred")

	utils.WriteError(rr, err)

	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
}

func TestWrap(t *testing.T) {

	simpleHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		numberOfBytes, err := w.Write([]byte("Hello, World!"))
		assert.NoError(t, err)
		assert.Equal(t, 13, numberOfBytes)
	})

	customHeaderMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Header", "CustomValue")
			next.ServeHTTP(w, r)
		})
	}

	wrappedHandler := utils.Wrap(simpleHandler, customHeaderMiddleware)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rr, req)

	assert.Equal(t, "Hello, World!", rr.Body.String())
	assert.Equal(t, "CustomValue", rr.Header().Get("X-Custom-Header"))
}

func TestWrap_MultipleMiddlewares(t *testing.T) {
	simpleHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		numberOfBytes, err := w.Write([]byte("Hello, World!"))
		assert.NoError(t, err)
		assert.Equal(t, 13, numberOfBytes)
	})

	middleware1 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware-1", "Value1")
			next.ServeHTTP(w, r)
		})
	}

	middleware2 := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Middleware-2", "Value2")
			next.ServeHTTP(w, r)
		})
	}

	wrappedHandler := utils.Wrap(simpleHandler, middleware1, middleware2)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rr, req)

	assert.Equal(t, "Hello, World!", rr.Body.String())
	assert.Equal(t, "Value1", rr.Header().Get("X-Middleware-1"))
	assert.Equal(t, "Value2", rr.Header().Get("X-Middleware-2"))
}
