// Package tracing wires distributed tracing.
//
// The value here is specific rather than general. In an orchestrator, the
// question that costs the most time to answer is "this instance has been stuck
// for two hours — on what?", and the answer usually lies in a connector call to
// somebody else's system. Metrics say a percentage of calls are slow; a trace
// says which instance, which node, which attempt, and where the time went.
//
// Off unless configured, and free when off: with no exporter the global provider
// is a no-op, so the instrumentation costs a nil check on the hot path.
package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	// envEndpoint turns tracing on by naming a collector, e.g.
	// "localhost:4318". Empty means tracing stays off.
	envEndpoint = "GOBPM_OTLP_ENDPOINT"

	// envInsecure sends to the collector over plain HTTP. Default is TLS,
	// because traces carry instance identifiers and node names.
	envInsecure = "GOBPM_OTLP_INSECURE"

	// envSampleRatio is the fraction of traces to keep, 0.0 to 1.0.
	//
	// The default is deliberately not 1.0. An engine executing thousands of
	// nodes a minute produces spans faster than most collectors will accept, and
	// the first thing an unconfigured deployment would notice is the trace
	// exporter becoming its own outage.
	envSampleRatio = "GOBPM_OTLP_SAMPLE_RATIO"

	defaultSampleRatio = 0.05

	// shutdownTimeout bounds the flush of pending spans at exit. Losing a few
	// trailing spans is better than delaying shutdown.
	shutdownTimeout = 5 * time.Second

	serviceName = "gobpm"
)

// tracerName identifies this instrumentation in the trace data.
const tracerName = "github.com/gsoultan/gobpm"

// Tracer returns the tracer everything in this codebase should use. When
// tracing is off this is a no-op tracer, so callers never need to check.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// Init installs the global tracer provider, returning a shutdown function.
//
// The shutdown function is always safe to call, including when tracing is off,
// so callers need no conditional cleanup.
func Init(ctx context.Context, version string) (func(context.Context), error) {
	endpoint := strings.TrimSpace(os.Getenv(envEndpoint))
	if endpoint == "" {
		log.Debug().Msg("Tracing is off; set " + envEndpoint + " to enable it")
		return func(context.Context) {}, nil
	}

	options := []otlptracehttp.Option{otlptracehttp.WithEndpoint(endpoint)}
	if insecureExport() {
		options = append(options, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(version),
	))
	if err != nil {
		return nil, fmt.Errorf("build trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// ParentBased keeps a trace whole: once a request is sampled, every span
		// beneath it is kept. Sampling each span independently would produce
		// traces with holes, which are worse than no trace at all.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampleRatio()))),
	)

	otel.SetTracerProvider(provider)
	// Without a propagator, an incoming traceparent header is ignored and this
	// service starts a new trace instead of joining the caller's.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	log.Info().Str("endpoint", endpoint).Float64("sampleRatio", sampleRatio()).Msg("Tracing enabled")

	return func(shutdownCtx context.Context) {
		shutdownCtx, cancel := context.WithTimeout(shutdownCtx, shutdownTimeout)
		defer cancel()
		if err := provider.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Could not flush pending traces at shutdown")
		}
	}, nil
}

func insecureExport() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envInsecure)), "true")
}

// sampleRatio reads the configured sampling fraction, clamped to [0,1].
func sampleRatio() float64 {
	raw := strings.TrimSpace(os.Getenv(envSampleRatio))
	if raw == "" {
		return defaultSampleRatio
	}

	var ratio float64
	if _, err := fmt.Sscanf(raw, "%f", &ratio); err != nil {
		log.Warn().Str("env", envSampleRatio).Str("value", raw).
			Msg("Ignoring invalid trace sample ratio")
		return defaultSampleRatio
	}
	return min(max(ratio, 0), 1)
}

// Attribute keys for the fields execution-plan.md §3.4 requires on every
// external call. They are constants because a span attribute that is spelled
// two ways is two attributes, and neither can be queried reliably.
const (
	AttrConnectorKey = attribute.Key("gobpm.connector.key")
	AttrInstanceID   = attribute.Key("gobpm.instance.id")
	AttrNodeID       = attribute.Key("gobpm.node.id")
	AttrAttempt      = attribute.Key("gobpm.attempt")
	AttrDefinitionID = attribute.Key("gobpm.definition.id")
)
