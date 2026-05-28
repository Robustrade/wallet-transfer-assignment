// Package tracing wires the OpenTelemetry SDK so the service emits
// spans without a Tempo / Jaeger / OTLP collector having to exist.
//
// Design choices:
//
//   - The default exporter is stdouttrace — spans are emitted as
//     pretty-printed JSON to the writer passed into Setup. The
//     binary uses stderr; tests use io.Discard so spans don't
//     pollute test output.
//
//   - When OTEL_EXPORTER_OTLP_ENDPOINT is set we could plug in an
//     OTLP exporter — left as a follow-up because the stdout path
//     already proves the wiring works. Adding OTLP later is a
//     constructor swap, not an architectural change.
//
//   - The package exposes a small Tracer() helper that returns the
//     project-named otel.Tracer. Call sites do
//     `ctx, span := tracing.Tracer().Start(ctx, "name")` and that
//     is the entirety of the API consumers need to know about.
package tracing

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// ServiceName is the value used for the service.name resource
// attribute. Centralised so process + tracer + metric labels stay
// aligned.
const ServiceName = "wallet-transfer-service"

// tracerName is the instrumentation library name passed to
// otel.Tracer. It identifies which package emitted the span.
const tracerName = "github.com/PratikDhanave/wallet-transfer-assignment"

// Setup initialises a TracerProvider backed by the stdout exporter
// and registers it as the global provider. It returns a shutdown
// function the caller MUST defer; without that flush, spans buffered
// in memory at process exit are lost.
//
// `out` is where spans are written (os.Stderr for the running
// binary, io.Discard for tests).
func Setup(ctx context.Context, out io.Writer) (shutdown func(context.Context) error, err error) {
	exporter, err := stdouttrace.New(
		stdouttrace.WithWriter(out),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, fmt.Errorf("stdouttrace.New: %w", err)
	}

	// A Resource is the set of attributes (service name, version,
	// host, env, etc.) attached to every span. OTel collectors use
	// these to group spans by service.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(ServiceName),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource.New: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		// BatchSpanProcessor batches spans for efficient export.
		// For the stdout exporter this is mild optimisation; for
		// a future OTLP exporter it's mandatory.
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	slog.Info("tracing initialised", "service", ServiceName, "exporter", "stdouttrace")
	return tp.Shutdown, nil
}

// Tracer returns the project's named OTel tracer. Call sites:
//
//	ctx, span := tracing.Tracer().Start(ctx, "TransferService.Create")
//	defer span.End()
//
// Always-safe to call: if Setup has not been invoked, otel returns
// a no-op tracer that records nothing, which is the correct default
// for tests and library use.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}
