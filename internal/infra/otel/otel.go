package otel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"forge-server/internal/config"

	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type ShutdownFunc func(context.Context) error

func NewTracerProvider(ctx context.Context, cfg config.TelemetryConfig) (ShutdownFunc, error) {
	if !cfg.Enabled {
		return func(context.Context) error { return nil }, nil
	}

	traceExporterOptions := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(normalizeOTLPEndpoint(cfg.OTLPEndpoint, "/v1/traces")),
	}
	if len(cfg.OTLPHeaders) > 0 {
		traceExporterOptions = append(traceExporterOptions, otlptracehttp.WithHeaders(cfg.OTLPHeaders))
	}

	traceExporter, err := otlptracehttp.New(ctx, traceExporterOptions...)
	if err != nil {
		return nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			attribute.String("service.name", cfg.ServiceName),
			attribute.String("service.version", cfg.ServiceVersion),
			attribute.String("deployment.environment.name", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create otel resource: %w", err)
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)

	logExporterOptions := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(normalizeOTLPEndpoint(cfg.OTLPLogEndpoint, "/v1/logs")),
	}
	if len(cfg.OTLPLogHeaders) > 0 {
		logExporterOptions = append(logExporterOptions, otlploghttp.WithHeaders(cfg.OTLPLogHeaders))
	}

	logExporter, err := otlploghttp.New(ctx, logExporterOptions...)
	if err != nil {
		return nil, fmt.Errorf("create otlp log exporter: %w", err)
	}

	logProvider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		sdklog.WithResource(res),
	)

	otelapi.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	otelapi.SetTracerProvider(traceProvider)
	logglobal.SetLoggerProvider(logProvider)

	return func(ctx context.Context) error {
		return errors.Join(
			logProvider.Shutdown(ctx),
			traceProvider.Shutdown(ctx),
		)
	}, nil
}

func normalizeOTLPEndpoint(endpoint string, defaultPath string) string {
	value := strings.TrimSpace(endpoint)
	if value == "" {
		value = "http://127.0.0.1:4318"
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return value
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = defaultPath
	}

	return parsed.String()
}
