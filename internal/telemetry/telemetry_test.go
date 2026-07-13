package telemetry

import (
	"context"
	"testing"
	"time"

	"wacalls/internal/voip/core"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNoEndpointIsNoop(t *testing.T) {
	shutdown, factory, tracer, err := Init(context.Background(), Config{})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, ok := tracer.(nopTracer); !ok {
		t.Fatalf("expected nopTracer with no endpoint, got %T", tracer)
	}
	obs := factory("c1")
	obs.AddMem(1)
	obs.TrackGoroutine()()
	tracer.StartCall("c1", CallAttrs{})
	tracer.EndCall("c1", "completed", "", 0)
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestCallTracerAndObserverRecord(t *testing.T) {
	spanExp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	inst, err := newInstruments(mp.Meter("wacalls"))
	if err != nil {
		t.Fatalf("instruments: %v", err)
	}
	tracer := newTracer(tp, inst)
	obs := newObserver(inst, "c1")

	tracer.StartCall("c1", CallAttrs{Session: "s1", Peer: "p", Direction: "outbound"})
	obs.AddMem(1000)
	done := obs.TrackGoroutine()
	obs.Mark(core.MarkTransportICE)
	tracer.MarkActive("c1", 250*time.Millisecond)
	done()
	obs.ReleaseMem(1000)
	tracer.EndCall("c1", "completed", "user_ended", 5*time.Second)
	tracer.EndCall("c1", "completed", "user_ended", 5*time.Second) // idempotent: must not panic or double-count

	spans := spanExp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected exactly 1 root span, got %d", len(spans))
	}
	if spans[0].Name != "call" {
		t.Fatalf("expected span name 'call', got %q", spans[0].Name)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	for _, want := range []string{"call.tracked_alloc.bytes", "call.goroutines", "call.phase.duration", "call.time_to_active", "calls.total", "calls.active", "call.duration"} {
		if !names[want] {
			t.Errorf("missing instrument %q in collected metrics", want)
		}
	}
}
