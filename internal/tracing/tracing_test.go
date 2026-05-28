package tracing

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

// TestSetup_RegistersGlobalProvider verifies that Setup installs the
// SDK as otel's global tracer provider. After Setup, otel.Tracer()
// must return a tracer that actually records (not the no-op).
func TestSetup_RegistersGlobalProvider(t *testing.T) {
	buf := &bytes.Buffer{}
	shutdown, err := Setup(context.Background(), buf)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = shutdown(ctx)
	}()

	tr := otel.Tracer(tracerName)
	_, span := tr.Start(context.Background(), "TestSpan")
	span.End()

	// Force the batch processor to flush so the span lands in buf.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = shutdown(ctx)

	out := buf.String()
	if !strings.Contains(out, "TestSpan") {
		t.Fatalf("exported span not found in output: %s", out)
	}
	if !strings.Contains(out, ServiceName) {
		t.Fatalf("service.name resource attribute missing from span: %s", out)
	}
}

// TestTracer_NoSetupReturnsNoOp verifies the safety net: calling
// Tracer() without Setup must NOT panic and must return a usable
// (no-op) tracer. Service code calls Tracer() unconditionally; if
// tests don't init the SDK, they should still build and run.
func TestTracer_NoSetupReturnsNoOp(t *testing.T) {
	// We do NOT call Setup. otel's default global provider is the
	// no-op provider, so any Start call should succeed and return
	// a non-nil span.
	_, span := Tracer().Start(context.Background(), "no-setup")
	if span == nil {
		t.Fatal("expected non-nil span even with no-op provider")
	}
	span.End()
}

// TestServiceName_IsExported is a regression test against someone
// renaming the constant — the resource attribute name shows up in
// dashboards and alerts; renaming it without coordination breaks
// observability infra.
func TestServiceName_IsExported(t *testing.T) {
	if ServiceName == "" {
		t.Fatal("ServiceName must be a non-empty string")
	}
	if !strings.Contains(ServiceName, "wallet") {
		t.Fatalf("ServiceName should mention wallet domain: %q", ServiceName)
	}
}
