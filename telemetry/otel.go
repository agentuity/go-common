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
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
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

type ShutdownFunc func()

func New(ctx context.Context, oltpServerURL string, authToken string, serviceName string) (logger.Logger, ShutdownFunc, error) {
	// parse oltpURL
	oltpURL, err := url.Parse(oltpServerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing oltpServerURL: %w", err)
	}
	oltpURL.Path = "/v1/logs"
	logURL := oltpURL.String()
	// oltpURL.Path = "/v1/traces"
	// traceURL := oltpURL.String()
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
		return nil, nil, fmt.Errorf("error creating resource: %w", err)
	}

	headers := make(map[string]string)
	if authToken != "" {
		headers["Authorization"] = "Bearer " + authToken
	}
	logExporterOpts := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(logURL),
		otlploghttp.WithHeaders(headers),
		otlploghttp.WithTimeout(time.Second * 10),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	}
	if oltpURL.Scheme == "http" {
		logExporterOpts = append(logExporterOpts, otlploghttp.WithInsecure())
	}
	// log.NewSimpleProcessor()
	exporter, err := otlploghttp.New(
		ctx, // ctx is not used by the exporter
		logExporterOpts...,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating log exporter: %w", err)
	}

	logProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
	)

	otelsLogger := logProvider.Logger(serviceName)

	logger := logger.NewOtelLogger(otelsLogger, logger.LevelTrace)

	return logger, func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()
		logProvider.Shutdown(ctx)
	}, nil
}
