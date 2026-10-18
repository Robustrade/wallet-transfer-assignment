package middleware_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-transfer-assignment/internal/middleware"

	"github.com/gofiber/fiber/v3"
)

func TestRegisterSetsJSONContentTypeOnEveryResponse(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler})
	middleware.Register(app)
	app.Get("/ping", func(c fiber.Ctx) error { return c.SendString("{}") })

	for _, path := range []string{"/ping", "/missing"} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, fiber.MIMEApplicationJSON) {
			t.Errorf("%s: Content-Type = %q, want prefix %q", path, got, fiber.MIMEApplicationJSON)
		}
	}
}

func TestErrorHandlerUsesTheJSONErrorEnvelope(t *testing.T) {
	app := fiber.New(fiber.Config{ErrorHandler: middleware.ErrorHandler})
	middleware.Register(app)
	app.Get("/boom", func(fiber.Ctx) error { return errors.New("secret database detail") })
	app.Get("/teapot", func(fiber.Ctx) error { return fiber.NewError(fiber.StatusTeapot, "short and stout") })

	tests := []struct {
		path, wantBody string
		wantStatus     int
	}{
		{"/missing", `{"error":"Not Found"}`, http.StatusNotFound},
		{"/teapot", `{"error":"short and stout"}`, http.StatusTeapot},
		{"/boom", `{"error":"internal server error"}`, http.StatusInternalServerError},
	}
	for _, tc := range tests {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, tc.path, http.NoBody))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != tc.wantStatus || string(body) != tc.wantBody {
			t.Errorf("%s: got %d %s, want %d %s", tc.path, resp.StatusCode, body, tc.wantStatus, tc.wantBody)
		}
	}
}
