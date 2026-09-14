package observability

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"blob/server/internal/platform/requestid"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

func HTTP(service string, output io.Writer) func(http.Handler) http.Handler {
	var mu sync.Mutex
	return func(next http.Handler) http.Handler {
		recovery := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recover() != nil {
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
		access := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			result := httpsnoop.CaptureMetrics(recovery, w, r)
			_, route, found := strings.Cut(r.Pattern, " ")
			if !found {
				route = r.Pattern
			}
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.route", route))
			entry := struct {
				Timestamp string  `json:"timestamp"`
				Level     string  `json:"level"`
				Service   string  `json:"service"`
				RequestID string  `json:"request_id"`
				TraceID   string  `json:"trace_id"`
				Method    string  `json:"method"`
				Route     string  `json:"route"`
				Status    int     `json:"status"`
				Duration  float64 `json:"duration_ms"`
			}{time.Now().UTC().Format(time.RFC3339Nano), "INFO", service, requestid.FromContext(r.Context()), span.SpanContext().TraceID().String(), safeMethod(r.Method), route, result.Code, float64(result.Duration) / float64(time.Millisecond)}
			mu.Lock()
			defer mu.Unlock()
			_ = json.NewEncoder(output).Encode(entry)
		})
		return otelhttp.NewHandler(access, "http.request",
			otelhttp.WithTracerProvider(httpProvider{otel.GetTracerProvider()}),
			otelhttp.WithMeterProvider(noop.NewMeterProvider()))
	}
}

func safeMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
		return method
	default:
		return "_OTHER"
	}
}

// Filter before the SDK records anything: otelhttp defaults include raw paths,
// user agents, peer addresses and read/write error details.
type httpProvider struct{ trace.TracerProvider }
type httpTracer struct{ trace.Tracer }
type httpSpan struct{ trace.Span }

func (p httpProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return httpTracer{p.TracerProvider.Tracer(name, opts...)}
}

func (t httpTracer) Start(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	parent := trace.SpanContextFromContext(ctx).WithTraceState(trace.TraceState{})
	ctx = trace.ContextWithSpanContext(ctx, parent)
	cfg := trace.NewSpanStartConfig(opts...)
	ctx, span := t.Tracer.Start(ctx, "http.request", trace.WithSpanKind(cfg.SpanKind()), trace.WithAttributes(httpAttributes(cfg.Attributes())...))
	wrapped := httpSpan{span}
	return trace.ContextWithSpan(ctx, wrapped), wrapped
}

func httpAttributes(attrs []attribute.KeyValue) []attribute.KeyValue {
	safe := make([]attribute.KeyValue, 0, 3)
	for _, attr := range attrs {
		switch attr.Key {
		case "http.request.method":
			safe = append(safe, attribute.String(string(attr.Key), safeMethod(attr.Value.AsString())))
		case "http.route", "http.response.status_code":
			safe = append(safe, attr)
		}
	}
	return safe
}

func (s httpSpan) SetAttributes(attrs ...attribute.KeyValue) {
	s.Span.SetAttributes(httpAttributes(attrs)...)
}
func (s httpSpan) SetStatus(code codes.Code, _ string)     { s.Span.SetStatus(code, "") }
func (s httpSpan) SetName(string)                          {}
func (s httpSpan) AddEvent(string, ...trace.EventOption)   {}
func (s httpSpan) RecordError(error, ...trace.EventOption) {}

// Calling End indirectly prevents the SDK from recovering and recording panic values.
func (s httpSpan) End(opts ...trace.SpanEndOption) { s.Span.End(opts...) }
