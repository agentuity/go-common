package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/agentuity/go-common/logger"
	"github.com/xhit/go-str2duration/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
)

func GenerateOTLPBearerToken(sharedSecret string, token string) (string, error) {
	hash := sha256.New()
	if _, err := hash.Write([]byte(sharedSecret + "." + token)); err != nil {
		return "", fmt.Errorf("error hashing token: %w", err)
	}
	secret := hash.Sum(nil)
	tok2 := base64.StdEncoding.EncodeToString(secret)
	return token + "." + tok2, nil
}

func GenerateOTLPBearerTokenWithExpiration(sharedSecret string, tokenVal string, expiration time.Time) (string, error) {
	hash := sha256.New()
	exp := time.Until(expiration)
	if exp < 0 {
		return "", fmt.Errorf("expiration time is in the past")
	}
	token := str2duration.String(exp.Round(time.Hour)) + "." + tokenVal
	if _, err := hash.Write([]byte(sharedSecret + "." + token)); err != nil {
		return "", fmt.Errorf("error hashing token: %w", err)
	}
	secret := hash.Sum(nil)
	tok2 := base64.StdEncoding.EncodeToString(secret)
	return token + "." + tok2, nil
}

type ShutdownFunc func()

func new(ctx context.Context, oltpServerURL string, authToken string, serviceName string) (context.Context, logger.Logger, ShutdownFunc, error) {
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

	res, err := resource.New(
		ctx,
		resource.WithFromEnv(),      // Discover and provide attributes from OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME environment variables.
		resource.WithTelemetrySDK(), // Discover and provide information about the OpenTelemetry SDK used.
		resource.WithProcess(),      // Discover and provide process information.
		resource.WithOS(),           // Discover and provide OS information.
		resource.WithContainer(),    // Discover and provide container information.
		resource.WithHost(),         // Discover and provide host information.
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if errors.Is(err, resource.ErrPartialResource) || errors.Is(err, resource.ErrSchemaURLConflict) {
		fmt.Println(err)
	} else if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating resource: %w", err)
	}

	headers := make(map[string]string)
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}

	// Setup log exporter
	logExporterOpts := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(logURL),
		otlploghttp.WithHeaders(headers),
		otlploghttp.WithTimeout(time.Second * 10),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	}
	if oltpURL.Scheme == "http" {
		logExporterOpts = append(logExporterOpts, otlploghttp.WithInsecure())
	}
	logExporter, err := otlploghttp.New(ctx, logExporterOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating log exporter: %w", err)
	}

	// Setup trace exporter
	traceExporterOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpointURL(traceURL),
		otlptracehttp.WithHeaders(headers),
		otlptracehttp.WithTimeout(time.Second * 10),
		otlptracehttp.WithCompression(otlptracehttp.GzipCompression),
	}
	if oltpURL.Scheme == "http" {
		traceExporterOpts = append(traceExporterOpts, otlptracehttp.WithInsecure())
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

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*11)
		defer cancel()
		logProvider.Shutdown(ctx)
		tracerProvider.Shutdown(ctx)
	}

	return ctx,
		logger.NewOtelLogger(otelsLogger, logger.LevelTrace),
		shutdown,
		nil
}

func New(ctx context.Context, serviceName string, telemetrySecret string, telemetryURL string, consoleLogger logger.Logger) (context.Context, logger.Logger, func(), error) {
	token, err := GenerateOTLPBearerToken(telemetrySecret, serviceName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error generating otel token: %w", err)
	}
	ctx2, olog, shutdownMetrics, err := new(ctx, telemetryURL, token, serviceName)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("error creating otel logger: %w", err)
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
