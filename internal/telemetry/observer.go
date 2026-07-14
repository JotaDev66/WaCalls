package telemetry

import (
	"context"
	"time"

	"wacalls/internal/voip/core"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type instruments struct {
	phaseDur     metric.Float64Histogram
	srtpDrops    metric.Int64Counter
	rtt          metric.Float64Histogram
	jitter       metric.Float64Histogram
	loss         metric.Float64Histogram
	timeToActive metric.Float64Histogram
	callMem      metric.Int64UpDownCounter
	callGor      metric.Int64UpDownCounter
	callsTotal   metric.Int64Counter
	callsFailed  metric.Int64Counter
	callsActive  metric.Int64UpDownCounter
	callDuration metric.Float64Histogram
}

func newInstruments(m metric.Meter) (*instruments, error) {
	in := &instruments{}
	var err error
	if in.phaseDur, err = m.Float64Histogram("call.phase.duration", metric.WithUnit("ms")); err != nil {
		return nil, err
	}
	if in.srtpDrops, err = m.Int64Counter("call.srtp.recv_drops",
		metric.WithUnit("{packet}"),
		metric.WithDescription("Inbound SRTP packets dropped before decode, by reason (replay, auth_failed, decryption, packet_too_short, other).")); err != nil {
		return nil, err
	}
	if in.rtt, err = m.Float64Histogram("call.rtt", metric.WithUnit("ms"),
		metric.WithDescription("Round-trip time from the peer's echoed report block (best-effort; only when the peer reports on us).")); err != nil {
		return nil, err
	}
	if in.jitter, err = m.Float64Histogram("call.jitter", metric.WithUnit("ms"),
		metric.WithDescription("Interarrival jitter of the inbound peer RTP stream (RFC 3550).")); err != nil {
		return nil, err
	}
	if in.loss, err = m.Float64Histogram("call.loss", metric.WithUnit("1"),
		metric.WithDescription("Cumulative fraction of inbound peer RTP packets lost (0..1).")); err != nil {
		return nil, err
	}
	if in.timeToActive, err = m.Float64Histogram("call.time_to_active", metric.WithUnit("ms")); err != nil {
		return nil, err
	}
	if in.callMem, err = m.Int64UpDownCounter("call.tracked_alloc.bytes",
		metric.WithUnit("By"),
		metric.WithDescription("Known per-call allocations tracked for leak detection (returns to zero at call end). NOT real RAM - use go.memory.used / calls.active for real memory.")); err != nil {
		return nil, err
	}
	if in.callGor, err = m.Int64UpDownCounter("call.goroutines",
		metric.WithDescription("Live goroutines held by the call; returns to zero at call end.")); err != nil {
		return nil, err
	}
	if in.callsTotal, err = m.Int64Counter("calls.total"); err != nil {
		return nil, err
	}
	if in.callsFailed, err = m.Int64Counter("calls.failed"); err != nil {
		return nil, err
	}
	if in.callsActive, err = m.Int64UpDownCounter("calls.active"); err != nil {
		return nil, err
	}
	if in.callDuration, err = m.Float64Histogram("call.duration", metric.WithUnit("ms")); err != nil {
		return nil, err
	}
	return in, nil
}

type otelObserver struct {
	inst  *instruments
	attrs metric.MeasurementOption
	start time.Time
}

func newObserver(inst *instruments, callID string) *otelObserver {
	return &otelObserver{
		inst:  inst,
		attrs: metric.WithAttributes(attribute.String("call_id", callID)),
		start: time.Now(),
	}
}

func (o *otelObserver) Mark(event string) {
	o.inst.phaseDur.Record(context.Background(), float64(time.Since(o.start).Milliseconds()),
		metric.WithAttributes(attribute.String("phase", event)))
}
func (o *otelObserver) SrtpRecvDrop(reason string) {
	o.inst.srtpDrops.Add(context.Background(), 1, o.attrs,
		metric.WithAttributes(attribute.String("reason", reason)))
}
func (o *otelObserver) NoteQuality(q core.CallQuality) {
	if q.HasRtt {
		o.inst.rtt.Record(context.Background(), q.RttMs, o.attrs)
	}
	o.inst.jitter.Record(context.Background(), q.JitterMs, o.attrs)
	o.inst.loss.Record(context.Background(), q.LossFraction, o.attrs)
}
func (o *otelObserver) AddMem(b int64)     { o.inst.callMem.Add(context.Background(), b, o.attrs) }
func (o *otelObserver) ReleaseMem(b int64) { o.inst.callMem.Add(context.Background(), -b, o.attrs) }
func (o *otelObserver) TrackGoroutine() func() {
	o.inst.callGor.Add(context.Background(), 1, o.attrs)
	return func() { o.inst.callGor.Add(context.Background(), -1, o.attrs) }
}
func (o *otelObserver) End(string, string) {}

var _ core.CallObserver = (*otelObserver)(nil)
