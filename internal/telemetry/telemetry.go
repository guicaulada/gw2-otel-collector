// Package telemetry bootstraps the OpenTelemetry SDK: a resource, an OTLP/HTTP
// metric exporter, and a periodic reader. The exporter honors the standard
// OTEL_EXPORTER_OTLP_* environment variables; only the endpoint is set
// explicitly from config so dev/Alloy/Grafana-Cloud is a config-only switch.
package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/guicaulada/gw2-otel-collector/internal/config"
)

// Setup configures the global meter provider and returns a shutdown function
// that flushes and stops it. Call the shutdown function before exit.
func Setup(ctx context.Context, cfg *config.Config) (func(context.Context) error, error) {
	exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(cfg.OTLPEndpointURL))
	if err != nil {
		return nil, fmt.Errorf("create OTLP metric exporter: %w", err)
	}

	// Add our service.* attributes schemaless so they inherit the default
	// resource's schema URL (the SDK's) instead of conflicting with it.
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("service.instance.id", cfg.ServiceInstance),
	))
	if err != nil {
		return nil, fmt.Errorf("build resource: %w", err)
	}

	reader := sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(cfg.ExportInterval))
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(reader),
	)
	otel.SetMeterProvider(mp)

	return mp.Shutdown, nil
}
