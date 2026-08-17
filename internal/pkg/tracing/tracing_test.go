package tracing

import (
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestTracerIsSafeWhenTracingIsOff is the property the call sites depend on:
// instrumentation must not need a "is tracing on" check around it, or the checks
// outnumber the spans and someone eventually forgets one.
func TestTracerIsSafeWhenTracingIsOff(t *testing.T) {
	// No provider installed — the global default is a no-op.
	ctx, span := Tracer().Start(t.Context(), "does nothing")
	span.SetAttributes(AttrConnectorKey.String("http-json"))
	span.End()

	if ctx == nil {
		t.Fatal("Start returned a nil context with tracing off")
	}
	if span.SpanContext().IsValid() {
		t.Error("a no-op tracer produced a recording span")
	}
}

// TestSpanCarriesTheRequiredAttributes pins the fields execution-plan.md §3.4
// requires on an external call. They are what make a trace answerable: without
// instance and node, a slow connector span says something is slow but not which
// process is stuck on it.
func TestSpanCarriesTheRequiredAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(sdktrace.NewTracerProvider()) })

	_, span := Tracer().Start(t.Context(), "connector.execute")
	span.SetAttributes(
		AttrConnectorKey.String("salesforce.create-lead"),
		AttrInstanceID.String("instance-1"),
		AttrNodeID.String("Activity_1x2y"),
		AttrAttempt.Int(2),
	)
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(ended))
	}

	got := make(map[attribute.Key]attribute.Value, len(ended[0].Attributes()))
	for _, kv := range ended[0].Attributes() {
		got[kv.Key] = kv.Value
	}

	for _, key := range []attribute.Key{AttrConnectorKey, AttrInstanceID, AttrNodeID, AttrAttempt} {
		if _, ok := got[key]; !ok {
			t.Errorf("span is missing %q, which §3.4 requires", key)
		}
	}
	if got[AttrAttempt].AsInt64() != 2 {
		t.Errorf("attempt = %d, want 2", got[AttrAttempt].AsInt64())
	}
}

func TestSampleRatio(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  float64
	}{
		{"unset falls back to the default", "", defaultSampleRatio},
		{"a valid ratio is honoured", "0.5", 0.5},
		{"everything", "1", 1},
		{"nothing", "0", 0},
		{"above one is clamped", "7", 1},
		{"below zero is clamped", "-1", 0},
		{"nonsense falls back rather than sampling nothing", "half", defaultSampleRatio},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				t.Setenv(envSampleRatio, tc.value)
			}
			if got := sampleRatio(); got != tc.want {
				t.Errorf("sampleRatio() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestInitIsANoOpWithoutAnEndpoint keeps tracing off by default, and keeps the
// returned shutdown safe to call regardless.
func TestInitIsANoOpWithoutAnEndpoint(t *testing.T) {
	t.Setenv(envEndpoint, "")

	shutdown, err := Init(t.Context(), "test")
	if err != nil {
		t.Fatalf("Init with no endpoint: %v", err)
	}
	if shutdown == nil {
		t.Fatal("Init returned no shutdown function")
	}
	shutdown(t.Context())
}
