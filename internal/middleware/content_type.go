package middleware

import (
	"errors"

	"github.com/gofiber/fiber/v3"
)

// Register wires global middleware for the application.
func Register(app *fiber.App) {
	RegisterWithConfig(app)
}

// RegisterWithConfig wires global middleware.
func RegisterWithConfig(app *fiber.App) {
	app.Use(JSONContentType)
}

// JSONContentType sets the default response content type.
func JSONContentType(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Next()
}

// ErrorHandler renders errors raised by Fiber itself (unknown route, wrong
// method, oversized body) in the API's JSON error envelope, instead of Fiber's
// plain-text default. Unexpected errors never expose their message.
func ErrorHandler(c fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	message := "internal server error"

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		status = fiberErr.Code
		message = fiberErr.Message
	}

	return c.Status(status).JSON(fiber.Map{"error": message})
}
