package observability_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
	"blob/server/internal/platform/observability"
	"blob/server/internal/platform/requestid"
	"blob/server/internal/transport/httpapi"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestHTTPLogAllowlist(t *testing.T) {
	shutdown, err := observability.Setup(t.Context(), "blob-api", "")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	var logs bytes.Buffer
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	handler := requestid.Middleware(observability.HTTP("blob-api", &logs)(mux))
	req := httptest.NewRequest(http.MethodGet, "/items/private-path?token=query-secret", strings.NewReader("top-secret"))
	req.Header.Set("Cookie", "session=cookie-secret")
	req.Header.Set("Authorization", "Bearer auth-secret")
	req.Header.Set("X-Request-ID", "request-secret")
	req.Header.Set("Baggage", "email=baggage-secret")
	req.Header.Set("Traceparent", "trace-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	for _, secret := range []string{"private-path", "query-secret", "top-secret", "cookie-secret", "auth-secret", "request-secret", "baggage-secret", "trace-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log leaked %q", secret)
		}
	}
	var fields map[string]any
	if err := json.Unmarshal(logs.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	wantKeys := []string{"duration_ms", "level", "method", "request_id", "route", "service", "status", "timestamp", "trace_id"}
	if !slices.Equal(keys, wantKeys) {
		t.Fatalf("log fields = %v, want %v", keys, wantKeys)
	}
	if fields["method"] != "GET" || fields["route"] != "/items/{id}" || fields["status"] != float64(202) || fields["service"] != "blob-api" || fields["level"] != "INFO" {
		t.Fatalf("unexpected access log: %v", fields)
	}
	if fields["request_id"] != rec.Header().Get("X-Request-ID") || !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(fields["request_id"].(string)) {
		t.Fatal("missing safe request ID")
	}
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(fields["trace_id"].(string)) || fields["trace_id"] == strings.Repeat("0", 32) {
		t.Fatal("missing trace ID")
	}
	if fields["duration_ms"].(float64) < 0 || fields["timestamp"] == "" {
		t.Fatal("missing timing")
	}
}

type receiver struct {
	collector.UnimplementedTraceServiceServer
	exports        chan *collector.ExportTraceServiceRequest
	partialSuccess string
}

func (r *receiver) Export(_ context.Context, req *collector.ExportTraceServiceRequest) (*collector.ExportTraceServiceResponse, error) {
	r.exports <- req
	if r.partialSuccess != "" {
		return &collector.ExportTraceServiceResponse{PartialSuccess: &collector.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: r.partialSuccess}}, nil
	}
	return &collector.ExportTraceServiceResponse{}, nil
}

func collectorEndpoint(t *testing.T) (string, *receiver) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := &receiver{exports: make(chan *collector.ExportTraceServiceRequest, 10)}
	server := grpc.NewServer()
	collector.RegisterTraceServiceServer(server, r)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	return "http://" + listener.Addr().String(), r
}

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHTTPTraceReachesPostgresWithoutSecrets(t *testing.T) {
	endpoint, receiver := collectorEndpoint(t)
	shutdown, err := observability.Setup(t.Context(), "blob-api", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	var pingContext trace.SpanContext
	handler := httpapi.NewHandler(system.NewHealthChecker(pingerFunc(func(ctx context.Context) error {
		pingContext = trace.SpanContextFromContext(ctx)
		return errors.New("error-secret: password=credential-secret")
	})), nil, config.Identity{PublicOrigin: "https://example.test", BrowserKey: [32]byte{1}}, observability.HTTP("blob-api", &logs))
	const parentTrace = "0123456789abcdef0123456789abcdef"
	const parentSpan = "0123456789abcdef"
	for _, path := range []string{"/api/v1/health/ready?token=query-secret", "/private-path?token=query-secret"} {
		req := httptest.NewRequest(http.MethodGet, path, strings.NewReader("body-secret"))
		req.Header.Set("Traceparent", "00-"+parentTrace+"-"+parentSpan+"-00")
		req.Header.Set("Tracestate", "private=state-secret")
		req.Header.Set("Baggage", "email=baggage-secret")
		req.Header.Set("Cookie", "session=cookie-secret")
		req.Header.Set("Authorization", "Bearer authorization-secret")
		req.Header.Set("User-Agent", "agent-secret")
		req.Host = "host-secret"
		req.Header.Set("X-Request-ID", parentTrace)
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	identitySecrets := []string{"identity-session-canary", "identity-context-canary", "identity-csrf-canary", "identity-cursor-canary"}
	key := [32]byte{1}
	identitySecrets = append(identitySecrets, base64.StdEncoding.EncodeToString(key[:]))
	var errorBodies strings.Builder
	for _, test := range []struct {
		method, path, code string
		status             int
	}{
		{http.MethodDelete, "/api/v1/identity/sessions/not-a-uuid", "invalid_request", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/identity/sessions?cursor=identity-cursor-canary", "dependency_unavailable", http.StatusServiceUnavailable},
	} {
		req := httptest.NewRequest(test.method, test.path, nil)
		req.Header.Set("Origin", "https://example.test")
		req.Header.Set("Cookie", "__Host-blob_session=identity-session-canary; __Host-blob_context=identity-context-canary")
		req.Header.Set("X-CSRF-Token", "identity-csrf-canary")
		req.Header.Set("Traceparent", "00-"+parentTrace+"-"+parentSpan+"-00")
		req.Header.Set("X-Request-ID", parentTrace)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != test.status || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("identity error status=%d headers=%v", rec.Code, rec.Header())
		}
		var body httpapi.ErrorResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Error.Code != test.code || body.Error.RequestId != parentTrace {
			t.Fatalf("unsafe identity envelope: %+v", body)
		}
		errorBodies.Write(rec.Body.Bytes())
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(flushCtx); err != nil {
		t.Fatal(err)
	}
	var exported *collector.ExportTraceServiceRequest
	select {
	case exported = <-receiver.exports:
	case <-flushCtx.Done():
		t.Fatal("configured exporter did not flush")
	}
	encoded, err := protojson.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"query-secret", "private-path", "body-secret", "error-secret", "credential-secret", "state-secret", "baggage-secret", "cookie-secret", "authorization-secret", "agent-secret", "host-secret"} {
		if strings.Contains(string(encoded)+logs.String(), secret) {
			t.Fatalf("telemetry leaked %q", secret)
		}
	}
	for _, secret := range identitySecrets {
		if strings.Contains(string(encoded)+logs.String()+errorBodies.String(), secret) {
			t.Fatalf("identity secret leaked: %q", secret)
		}
	}
	var spans []*tracepb.Span
	for _, resource := range exported.ResourceSpans {
		if len(resource.Resource.Attributes) != 1 || resource.Resource.Attributes[0].Key != "service.name" {
			t.Fatal("resource attributes escaped allowlist")
		}
		for _, scope := range resource.ScopeSpans {
			spans = append(spans, scope.Spans...)
		}
	}
	if len(spans) != 5 {
		t.Fatalf("exported %d spans, want four HTTP and one postgres", len(spans))
	}
	var ping, ready *tracepb.Span
	for _, span := range spans {
		if len(span.Events) != 0 || len(span.Links) != 0 || span.TraceState != "" || span.Status.GetMessage() != "" {
			t.Fatal("unexpected span details")
		}
		for _, attr := range span.Attributes {
			if !slices.Contains([]string{"http.request.method", "http.response.status_code", "http.route", "operation", "success"}, attr.Key) {
				t.Fatalf("unexpected span attribute %s", attr.Key)
			}
			if attr.Key == "http.route" && attr.Value.GetStringValue() == "/api/v1/health/ready" {
				ready = span
			}
		}
		if span.Name == "postgres.ping" {
			ping = span
		}
	}
	if ready == nil || ping == nil || !bytes.Equal(ping.ParentSpanId, ready.SpanId) || !bytes.Equal(ping.TraceId, ready.TraceId) || pingContext.TraceID().String() != parentTrace {
		t.Fatal("HTTP context did not reach postgres.ping")
	}
	decoder := json.NewDecoder(&logs)
	for _, want := range []struct {
		route  string
		status int
	}{{"/api/v1/health/ready", 503}, {"", 404}} {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		if entry["route"] != want.route || entry["status"] != float64(want.status) || entry["request_id"] != parentTrace || entry["trace_id"] != parentTrace {
			t.Fatalf("unexpected access correlation: %v", entry)
		}
	}
}

func TestEmptyEndpointNeverSamplesOrExports(t *testing.T) {
	endpoint, receiver := collectorEndpoint(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", endpoint)
	shutdown, err := observability.Setup(t.Context(), "blob-api", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := trace.ContextWithRemoteSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, Remote: true,
	}))
	_, span := otel.Tracer("test").Start(ctx, "local")
	if !span.SpanContext().IsValid() || span.IsRecording() || span.SpanContext().IsSampled() {
		t.Fatal("empty endpoint must use NeverSample even with a sampled parent")
	}
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-receiver.exports:
		t.Fatal("empty endpoint exported")
	default:
	}
}

func TestHTTPPanicDoesNotRecordErrorDetails(t *testing.T) {
	endpoint, receiver := collectorEndpoint(t)
	shutdown, err := observability.Setup(t.Context(), "blob-api", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var logs, serverErrors bytes.Buffer
	handler := requestid.Middleware(observability.HTTP("blob-api", &logs)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Partial", "header-secret")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"private":"body-secret"}`))
		panic("panic-secret")
	})))
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(&serverErrors, "", 0)
	server.Start()
	defer server.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("panic must return a generic 500: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 500 || string(body) != "Internal Server Error\n" {
		t.Fatalf("unexpected panic response: status=%d", response.StatusCode)
	}
	if response.Header.Get("X-Partial") != "" || response.Header.Get("X-Request-ID") == "" {
		t.Fatal("panic response leaked partial headers or lost request ID")
	}
	var access map[string]any
	if err := json.Unmarshal(logs.Bytes(), &access); err != nil {
		t.Fatal(err)
	}
	if access["status"] != float64(500) {
		t.Fatal("panic access status was not 500")
	}
	if serverErrors.Len() != 0 || strings.Contains(logs.String()+string(body), "secret") {
		t.Fatal("panic details leaked into response or logs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case exported := <-receiver.exports:
		encoded, err := protojson.Marshal(exported)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "panic-secret") {
			t.Fatal("panic details leaked into trace")
		}
	case <-ctx.Done():
		t.Fatal("panic span was not flushed")
	}
}

func TestHTTPRecoveryPreservesNormalResponse(t *testing.T) {
	shutdown, err := observability.Setup(t.Context(), "blob-api", "")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	var logs bytes.Buffer
	handler := requestid.Middleware(observability.HTTP("blob-api", &logs)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("X-Multiple", "first")
		w.Header().Add("X-Multiple", "second")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusCreated || recorder.Body.String() != `{"status":"ok"}` || recorder.Header().Get("Content-Type") != "application/json" || !slices.Equal(recorder.Header().Values("X-Multiple"), []string{"first", "second"}) || recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("normal response was changed by recovery")
	}
}

func TestSDKConfigDiagnostics(t *testing.T) {
	if mode := os.Getenv("BLOB_DIAGNOSTIC_TEST"); mode != "" {
		endpoint := "https://credential-secret@collector.invalid/%zz?token=query-secret"
		if mode == "exporter" {
			var receiver *receiver
			endpoint, receiver = collectorEndpoint(t)
			receiver.partialSuccess = "collector-secret: https://credential-secret@collector.invalid?token=query-secret"
		}
		shutdown, err := observability.Setup(t.Context(), "blob-api", endpoint)
		if err != nil {
			t.Fatal("unexpected setup failure")
		}
		if mode == "exporter" {
			_, span := otel.Tracer("test").Start(t.Context(), "test.export")
			span.End()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			otel.Handle(err)
		}
		return
	}
	for _, mode := range []string{"config", "exporter"} {
		t.Run(mode, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run=^TestSDKConfigDiagnostics$")
			command.Env = append(os.Environ(), "BLOB_DIAGNOSTIC_TEST="+mode)
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("diagnostic subprocess failed: %v", err)
			}
			if strings.Contains(string(output), "secret") || strings.Contains(string(output), "collector.invalid") {
				t.Fatal("SDK diagnostic leaked error, endpoint, or attributes")
			}
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				if !strings.HasPrefix(line, "{") {
					continue
				}
				var event map[string]any
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatal(err)
				}
				if len(event) != 4 || event["event"] != "telemetry_failure" || event["service"] != "blob-api" || event["level"] != "ERROR" {
					t.Fatal("SDK diagnostic escaped the allowlist")
				}
				found = true
			}
			if !found {
				t.Fatal("SDK failure produced no safe diagnostic")
			}
		})
	}
}

func TestTelemetryDiagnosticAllowlist(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	previousStderr := os.Stderr
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = previousStderr; _ = writer.Close() })
	shutdown, err := observability.Setup(t.Context(), "blob-api", "")
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())
	otel.Handle(errors.New("export-secret: https://credential-secret@collector.invalid?token=signed-secret"))
	_ = writer.Close()
	os.Stderr = previousStderr
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"export-secret", "credential-secret", "signed-secret", "collector.invalid"} {
		if bytes.Contains(output, []byte(secret)) {
			t.Fatalf("diagnostic leaked %q", secret)
		}
	}
	var event map[string]any
	if err := json.Unmarshal(output, &event); err != nil {
		t.Fatalf("expected a structured diagnostic: %v", err)
	}
	if len(event) != 4 || event["event"] != "telemetry_failure" || event["level"] != "ERROR" || event["service"] != "blob-api" || event["timestamp"] == nil {
		t.Fatalf("unexpected diagnostic fields: %v", event)
	}
}
