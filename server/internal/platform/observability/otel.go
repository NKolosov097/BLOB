package observability

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func Setup(ctx context.Context, serviceName, endpoint string) (func(context.Context) error, error) {
	reportFailure := func() {
		_ = json.NewEncoder(os.Stderr).Encode(struct {
			Timestamp string `json:"timestamp"`
			Level     string `json:"level"`
			Service   string `json:"service"`
			Event     string `json:"event"`
		}{time.Now().UTC().Format(time.RFC3339Nano), "ERROR", serviceName, "telemetry_failure"})
	}
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) { reportFailure() }))
	otel.SetLogger(logr.New(diagnosticSink{reportFailure}))
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", serviceName))),
		sdktrace.WithSampler(sdktrace.NeverSample()),
	}
	if endpoint != "" {
		exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
		if err != nil {
			otel.Handle(err)
			return nil, errors.New("telemetry setup failed")
		}
		opts = append(opts, sdktrace.WithSampler(sdktrace.AlwaysSample()), sdktrace.WithBatcher(exporter))
	}
	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

// SDK logger arguments may contain endpoints, credentials, and arbitrary errors.
type diagnosticSink struct{ report func() }

func (diagnosticSink) Init(logr.RuntimeInfo)            {}
func (diagnosticSink) Enabled(level int) bool           { return level <= 1 }
func (s diagnosticSink) Info(int, string, ...any)       { s.report() }
func (s diagnosticSink) Error(error, string, ...any)    { s.report() }
func (s diagnosticSink) WithValues(...any) logr.LogSink { return s }
func (s diagnosticSink) WithName(string) logr.LogSink   { return s }
