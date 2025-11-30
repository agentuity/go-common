package telemetry

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/agentuity/go-common/authentication"
	"github.com/agentuity/go-common/logger"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

func GenerateOTLPBearerToken(sharedSecret string, token string) (string, error) {
	return authentication.NewBearerToken(sharedSecret)
}

func GenerateOTLPBearerTokenWithExpiration(sharedSecret string, expiration time.Time) (string, error) {
	return authentication.NewBearerToken(sharedSecret, authentication.WithExpiration(expiration))
}

type ShutdownFunc func()

// Option is a functional option for configuring telemetry
type Option func(*config)

// config holds the configuration options for telemetry
type config struct {
	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	timeout     time.Duration
	headers     http.Header
}

// WithDialContext sets a custom dialer for HTTP connections
func WithDialContext(dial func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(c *config) {
		c.dialContext = dial
	}
}

// WithTimeout sets a custom timeout for HTTP connections
func WithTimeout(dur time.Duration) Option {
	return func(c *config) {
		c.timeout = dur
	}
}

// WithHeaders sets custom headers for HTTP connections
func WithHeaders(headers http.Header) Option {
	return func(c *config) {
		c.headers = headers
	}
}

func new(ctx context.Context, oltpServerURL string, authToken string, serviceName string, opts ...Option) (context.Context, logger.Logger, ShutdownFunc, error) {
	// Apply options
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.timeout <= 0 {
		cfg.timeout = 10 * time.Second
	}
	// parse oltpURL
	oltpURL, err := url.Parse(oltpServerURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error parsing oltpServerURL: %w", err)
	}
	oltpURL.Path = "/v1/logs"
	logURL := oltpURL.String()
	oltpURL.Path = "/v1/traces"
	traceURL := oltpURL.String()
	// oltpURL.Path = "/v1/metrics"
	// metricsURL := oltpURL.String()

	var kvs []attribute.KeyValue
	if val, ok := os.LookupEnv("AGENTUITY_REGION"); ok {
		kvs = append(kvs, attribute.KeyValue{Key: "@agentuity/region", Value: attribute.StringValue(val)})
	}
	if val, ok := os.LookupEnv("AGENTUITY_CLOUD_ID"); ok {
		kvs = append(kvs, attribute.KeyValue{Key: "@agentuity/cloudId", Value: attribute.StringValue(val)})
	}
	if val, ok := os.LookupEnv("AGENTUITY_CLOUDSTACK"); ok {
		kvs = append(kvs, attribute.KeyValue{Key: "@agentuity/cloudStack", Value: attribute.StringValue(val)})
	}
	if val, ok := os.LookupEnv("AGENTUITY_MACHINE_ID"); ok {
		kvs = append(kvs, attribute.KeyValue{Key: "@agentuity/machineId", Value: attribute.StringValue(val)})
	}

	kvs = append(kvs, semconv.ServiceName(serviceName))

	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),      // Discover and provide attributes from OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME environment variables.
		resource.WithTelemetrySDK(), // Discover and provide information about the OpenTelemetry SDK used.
		resource.WithProcess(),      // Discover and provide process information.
		resource.WithOS(),           // Discover and provide OS information.
		resource.WithContainer(),    // Discover and provide container information.
		resource.WithHost(),         // Discover and provide host information.
		resource.WithAttributes(kvs...),
	)
	if errors.Is(err, resource.ErrPartialResource) || errors.Is(err, resource.ErrSchemaURLConflict) {
		fmt.Println(err)
	} else if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating resource: %w", err)
	}

	headers := make(map[string]string)
	for k, v := range cfg.headers {
		headers[k] = strings.Join(v, ",")
	}
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}

	// Create HTTP client with custom dialer if provided
	var httpClient *http.Client
	if cfg.dialContext != nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.DialContext = cfg.dialContext
		transport.ForceAttemptHTTP2 = true
		httpClient = &http.Client{
			Transport: transport,
			Timeout:   cfg.timeout,
		}
	}

	// Setup log exporter
	logExporterOpts := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(logURL),
		otlploghttp.WithHeaders(headers),
		otlploghttp.WithTimeout(cfg.timeout),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	}
	if oltpURL.Scheme == "http" {
		logExporterOpts = append(logExporterOpts, otlploghttp.WithInsecure())
	}
	if httpClient != nil {
		logExporterOpts = append(logExporterOpts, otlploghttp.WithHTTPClient(httpClient))
	}
	logExporter, err := otlploghttp.New(ctx, logExporterOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating log exporter: %w", err)
	}

	// Setup trace exporter
	traceExporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(traceURL),
		otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithTimeout(cfg.timeout),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
	}
	if oltpURL.Scheme == "http" {
		traceExporterOpts = append(traceExporterOpts, otlptracehttp.WithInsecure())
	}
	if httpClient != nil {
		traceExporterOpts = append(traceExporterOpts, otlptracehttp.WithHTTPClient(httpClient))
	}
	traceExporter, err := otlptracehttp.New(ctx, traceExporterOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating trace exporter: %w", err)
	}

	// Create trace provider
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)

	logProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)

	otelsLogger := logProvider.Logger(serviceName)

	tc := propagation.TraceContext{}
	// Register the TraceContext propagator globally.
	otel.SetTextMapPropagator(tc)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout+time.Second)
		defer cancel()
		logProvider.Shutdown(ctx)
		tracerProvider.Shutdown(ctx)
		traceExporter.Shutdown(ctx)
		logExporter.Shutdown(ctx)
	}

	return ctx,
		logger.NewOtelLogger(otelsLogger, logger.LevelTrace),
		shutdown,
		nil
}

func New(ctx context.Context, serviceName string, telemetrySecret string, telemetryURL string, consoleLogger logger.Logger, opts ...Option) (context.Context, logger.Logger, func(), error) {
	token, err := GenerateOTLPBearerToken(telemetrySecret, serviceName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error generating otel token: %w", err)
	}
	ctx2, olog, shutdownMetrics, err := new(ctx, telemetryURL, token, serviceName, opts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating otel: %w", err)
	}
	if consoleLogger == nil {
		return ctx2, olog, shutdownMetrics, nil
	}
	return ctx2, olog.Stack(consoleLogger), shutdownMetrics, nil
}

func NewWithAPIKey(ctx context.Context, serviceName string, telemetryURL string, telemetryAPIKey string, consoleLogger logger.Logger, opts ...Option) (context.Context, logger.Logger, func(), error) {
	ctx2, olog, shutdownMetrics, err := new(ctx, telemetryURL, telemetryAPIKey, serviceName, opts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating otel: %w", err)
	}
	if consoleLogger == nil {
		return ctx2, olog, shutdownMetrics, nil
	}
	return ctx2, olog.Stack(consoleLogger), shutdownMetrics, nil
}

func StartSpan(ctx context.Context, logger logger.Logger, tracer trace.Tracer, spanName string, opts ...trace.SpanStartOption) (context.Context, logger.Logger, trace.Span) {
	ctx, span := tracer.Start(ctx, spanName, opts...)
	return ctx, logger.WithContext(ctx), span
}
