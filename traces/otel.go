package traces

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.18.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	// ServiceName is the name of the instrumented library/service
	ServiceName string `mapstructure:"tracing-config-service-name" description:"Set the tracing service name used in traces"`
	// ServiceVersion is the version of the instrumented library/service
	// It must be in Semver format `<MAYOR>.<MINOR>.<PATCH>`
	ServiceVersion string `mapstructure:"tracing-config-service-version" description:"Set the tracing service version used in traces"`
	// CollectorEndpoint is the target of the collector.
	// Must be in the format `<DOMAIN>:<PORT>` without prefixed protocol
	// Ignored in the case of a LocalProvider
	CollectorEndpoint string `mapstructure:"tracing-config-collector-endpoint" description:"Set the tracing collector endpoint used to send traces to the collector"`
}

// InitProvider creates an OpenTelemetry provider for the concrete service.
// If the collector in the destination endpoint isn't reachable, then the init function will return an error.
func InitProvider(ctx context.Context, config Config) (func(ctx context.Context) error, error) {
	connCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	client, err := grpc.NewClient(config.CollectorEndpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to collector: %w", err)
	}

	traceExporter, err := otlptracegrpc.New(connCtx, otlptracegrpc.WithGRPCConn(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	return config.initProvider(ctx, traceExporter)
}

// InitLocalProvider creates an OpenTelemetry provider for the concrete service.
// If exportToConsole is `true`, the traces will be written in the console for debugging purposes.
func InitLocalProvider(ctx context.Context, config Config, exportToConsole bool) (func(ctx context.Context) error, error) {
	fileTarget := io.Discard
	if exportToConsole {
		fileTarget = os.Stdout
	}

	traceExporter, err := stdouttrace.New(
		stdouttrace.WithWriter(fileTarget),
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	return config.initProvider(ctx, traceExporter)
}

func (c Config) initProvider(ctx context.Context, exporter sdkTrace.SpanExporter) (func(ctx context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(c.ServiceName),
			semconv.ServiceVersion(c.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resouce: %w", err)
	}

	bsp := sdkTrace.NewBatchSpanProcessor(exporter)
	tracerProvider := sdkTrace.NewTracerProvider(
		sdkTrace.WithSampler(sdkTrace.AlwaysSample()),
		sdkTrace.WithResource(res),
		sdkTrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tracerProvider.Shutdown, nil
}
