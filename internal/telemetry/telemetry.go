// Package telemetry bootstraps the OpenTelemetry SDK: a shared resource, an
// OTLP/HTTP metric exporter + periodic reader, and an OTLP/HTTP log exporter +
// batch processor. Exporters honor the standard OTEL_EXPORTER_OTLP_* env vars;
// only the endpoint is set explicitly so dev/Alloy/Grafana-Cloud is a
// config-only switch.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otellog "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/guicaulada/gw2-otel-collector/internal/config"
)

// Setup configures the global meter and logger providers and returns a shutdown
// function that flushes and stops both. Call the shutdown function before exit.
func Setup(ctx context.Context, cfg *config.Config) (func(context.Context) error, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("service.instance.id", cfg.ServiceInstance),
	))
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}

	// Expose the configured endpoint as the standard env var so each signal
	// exporter appends its own path (/v1/metrics, /v1/logs). Don't override an
	// endpoint the user set explicitly.
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", cfg.OTLPEndpointURL)
	}

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(cfg.ExportInterval))),
	)
	otel.SetMeterProvider(mp)

	logExp, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)
	otellog.SetLoggerProvider(lp)

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
	)
	otel.SetTracerProvider(tp)

	shutdown := func(ctx context.Context) error {
		return errors.Join(mp.Shutdown(ctx), lp.Shutdown(ctx), tp.Shutdown(ctx))
	}
	return shutdown, nil
}
