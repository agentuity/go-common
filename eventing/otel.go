package eventing

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

var tracer = otel.Tracer("@agentuity/go-common/eventing")

var propagator = propagation.TraceContext{}
