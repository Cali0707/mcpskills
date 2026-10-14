package server

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type telemetry struct {
	tracer         trace.Tracer
	readCounter    metric.Int64Counter
	traceProvider  *sdktrace.TracerProvider
	metricProvider *sdkmetric.MeterProvider
}

func newTelemetry(ctx context.Context) (*telemetry, error) {
	t := &telemetry{tracer: trace.NewNoopTracerProvider().Tracer(serverName)}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint != "" || os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		provider := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resource.NewWithAttributes("", attribute.String("service.name", serverName))),
		)
		t.tracer, t.traceProvider = provider.Tracer(serverName), provider
	}
	if endpoint != "" || os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" {
		exporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			t.shutdown()
			return nil, err
		}
		provider := sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
			sdkmetric.WithResource(resource.NewWithAttributes("", attribute.String("service.name", serverName))),
		)
		t.metricProvider = provider
		t.readCounter, err = provider.Meter(serverName).Int64Counter("mcpskills.resource.reads", metric.WithDescription("Successful skill file reads"), metric.WithUnit("{read}"))
		if err != nil {
			t.shutdown()
			return nil, err
		}
	}
	return t, nil
}

func (t *telemetry) shutdown() {
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if t.metricProvider != nil {
		_ = t.metricProvider.Shutdown(ctx)
	}
	if t.traceProvider != nil {
		_ = t.traceProvider.Shutdown(ctx)
	}
}
