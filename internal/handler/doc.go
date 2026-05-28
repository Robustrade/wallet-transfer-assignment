// Package handler implements the HTTP transport layer.
//
// Handlers are intentionally thin: they decode the request, invoke a
// single service method, and encode the response. All client-facing
// error translation flows through writeError in errors.go so HTTP status
// codes are mapped in exactly one place.
package handler
