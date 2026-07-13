package telemetry

import (
	"context"
	"os"
	"strings"

	"wacalls/internal/voip/core"

	runtimemetrics "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	Endpoint       string
	ServiceName    string
	ServiceVersion string
}

func ConfigFromEnv() Config {
	name := os.Getenv("OTEL_SERVICE_NAME")
	if name == "" {
		name = "wacalls"
	}
	return Config{
		Endpoint:    strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")),
		ServiceName: name,
	}
}

func nopFactory(string) core.CallObserver { return core.NopObserver{} }

func newResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{attribute.String("service.name", cfg.ServiceName)}
	if cfg.ServiceVersion != "" {
		attrs = append(attrs, attribute.String("service.version", cfg.ServiceVersion))
	}
	return resource.New(ctx, resource.WithAttributes(attrs...))
}

func Init(ctx context.Context, cfg Config) (func(context.Context) error, func(string) core.CallObserver, CallTracer, error) {
	if cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nopFactory, nopTracer{}, nil
	}

	res, err := newResource(ctx, cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(traceExp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, nil, nil, err
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)), sdkmetric.WithResource(res))
	otel.SetMeterProvider(mp)

	shutdown := func(ctx context.Context) error {
		err1 := mp.ForceFlush(ctx)
		if err2 := mp.Shutdown(ctx); err1 == nil {
			err1 = err2
		}
		if err3 := tp.Shutdown(ctx); err1 == nil {
			err1 = err3
		}
		return err1
	}

	if err := runtimemetrics.Start(runtimemetrics.WithMeterProvider(mp)); err != nil {
		_ = shutdown(ctx)
		return nil, nil, nil, err
	}

	inst, err := newInstruments(mp.Meter("wacalls"))
	if err != nil {
		_ = shutdown(ctx)
		return nil, nil, nil, err
	}

	return shutdown, func(callID string) core.CallObserver { return newObserver(inst, callID) }, newTracer(tp, inst), nil
}
