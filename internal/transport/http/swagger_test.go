package transporthttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSwaggerUIHandler_ServesHTML(t *testing.T) {
	server := NewServer(nil, nil, nil)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected text/html response, got %s", res.Header().Get("Content-Type"))
	}
	if !strings.Contains(res.Body.String(), "SwaggerUIBundle") {
		t.Fatal("expected Swagger UI HTML output")
	}
}

func TestOpenAPISpecHandler_ServesYaml(t *testing.T) {
	server := NewServer(nil, nil, nil)
	mux := http.NewServeMux()
	server.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	res := httptest.NewRecorder()
	mux.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", res.Code)
	}
	if !strings.Contains(res.Header().Get("Content-Type"), "application/yaml") {
		t.Fatalf("expected application/yaml response, got %s", res.Header().Get("Content-Type"))
	}
	if !strings.Contains(res.Body.String(), "openapi: 3.0.3") {
		t.Fatal("expected openapi: 3.0.3 in YAML spec")
	}
}
