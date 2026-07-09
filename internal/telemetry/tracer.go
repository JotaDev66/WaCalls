package telemetry

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type CallAttrs struct {
	Session   string
	Peer      string
	Direction string
	Video     bool
}

type CallTracer interface {
	StartCall(callID string, attrs CallAttrs)
	MarkActive(callID string, tta time.Duration)
	EndCall(callID, result, reason string, dur time.Duration)
}

type nopTracer struct{}

func (nopTracer) StartCall(string, CallAttrs)                   {}
func (nopTracer) MarkActive(string, time.Duration)              {}
func (nopTracer) EndCall(string, string, string, time.Duration) {}

type otelTracer struct {
	tracer trace.Tracer
	inst   *instruments
	mu     sync.Mutex
	spans  map[string]trace.Span
}

func newTracer(tp trace.TracerProvider, inst *instruments) *otelTracer {
	return &otelTracer{tracer: tp.Tracer("wacalls/call"), inst: inst, spans: map[string]trace.Span{}}
}

func (t *otelTracer) StartCall(callID string, a CallAttrs) {
	ctx := context.Background()
	_, span := t.tracer.Start(ctx, "call", trace.WithAttributes(
		attribute.String("call_id", callID),
		attribute.String("session", a.Session),
		attribute.String("peer", a.Peer),
		attribute.String("direction", a.Direction),
		attribute.Bool("video", a.Video),
	))
	t.mu.Lock()
	if _, exists := t.spans[callID]; exists {
		t.mu.Unlock()
		span.End()
		return
	}
	t.spans[callID] = span
	t.mu.Unlock()
	t.inst.callsTotal.Add(ctx, 1)
	t.inst.callsActive.Add(ctx, 1)
}

func (t *otelTracer) MarkActive(callID string, tta time.Duration) {
	t.mu.Lock()
	span := t.spans[callID]
	t.mu.Unlock()
	if span != nil {
		span.AddEvent("active", trace.WithAttributes(attribute.Int64("time_to_active_ms", tta.Milliseconds())))
	}
	t.inst.timeToActive.Record(context.Background(), float64(tta.Milliseconds()))
}

func (t *otelTracer) EndCall(callID, result, reason string, dur time.Duration) {
	t.mu.Lock()
	span, ok := t.spans[callID]
	if ok {
		delete(t.spans, callID)
	}
	t.mu.Unlock()
	if !ok {
		return
	}
	span.SetAttributes(attribute.String("result", result), attribute.String("reason", reason))
	span.End()
	ctx := context.Background()
	t.inst.callsActive.Add(ctx, -1)
	t.inst.callDuration.Record(ctx, float64(dur.Milliseconds()))
	if result == "failed" {
		t.inst.callsFailed.Add(ctx, 1)
	}
}
