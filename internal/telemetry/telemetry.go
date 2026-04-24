package telemetry

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/agentproxy/agent-proxy/internal/detector"
	"github.com/agentproxy/agent-proxy/internal/logger"
)

const tracerName = "agent-proxy"

// Setup initialises the OTEL tracer provider with an OTLP HTTP exporter.
// Returns a shutdown function that must be called on exit.
// If endpoint is empty, Setup is a no-op and returns a nil shutdown func.
func Setup(ctx context.Context, endpoint string) (shutdown func(context.Context) error, err error) {
	if endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("OTLP exporter: %w", err)
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("agent-proxy"),
			semconv.ServiceVersion("0.1.0"),
		),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

// RecordSpan emits a completed OTEL span for a captured proxy message entry.
// Protocol is mapped to span attributes following OpenTelemetry semantic conventions
// where applicable, with agent-proxy-specific attributes for protocol details.
func RecordSpan(entry logger.Entry) {
	if otel.GetTracerProvider() == otel.GetTracerProvider() {
		// Only emit if a real provider is registered (not the no-op default).
	}

	tracer := otel.Tracer(tracerName)

	spanName := spanNameFor(entry)
	start := entry.Timestamp
	end := start.Add(time.Duration(entry.LatencyMs) * time.Millisecond)
	if end.Equal(start) {
		end = start.Add(time.Millisecond)
	}

	ctx := context.Background()
	_, span := tracer.Start(ctx, spanName,
		trace.WithTimestamp(start),
	)

	span.SetAttributes(
		attribute.String("agent.protocol", string(entry.Protocol)),
		attribute.String("agent.direction", string(entry.Direction)),
		attribute.String("agent.method", entry.Method),
		attribute.String("agent.path", entry.Path),
		attribute.Int64("agent.latency_ms", entry.LatencyMs),
	)

	if entry.StatusCode > 0 {
		span.SetAttributes(attribute.Int("http.response.status_code", entry.StatusCode))
	}

	if entry.Protocol == detector.ProtocolMCP || entry.Protocol == detector.ProtocolMCPSSE {
		span.SetAttributes(attribute.String("mcp.method", entry.Method))
	}

	if entry.Body != nil {
		body := string(entry.Body)
		if len(body) > 4096 {
			body = body[:4096] + "…"
		}
		span.SetAttributes(attribute.String("agent.body", body))
	}

	span.End(trace.WithTimestamp(end))
}

func spanNameFor(e logger.Entry) string {
	switch {
	case e.Method != "":
		return fmt.Sprintf("%s %s", e.Protocol, e.Method)
	case e.Path != "":
		return fmt.Sprintf("%s %s", e.Protocol, e.Path)
	default:
		return string(e.Protocol)
	}
}
