package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
)

type pingerFunc func(context.Context) error

func (f pingerFunc) Ping(ctx context.Context) error {
	return f(ctx)
}

var testIdentityConfig = config.Identity{PublicOrigin: "https://example.test", BrowserKey: [32]byte{1}}

func TestLiveness(t *testing.T) {
	h := NewHandler(system.NewHealthChecker(nil), nil, testIdentityConfig)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal("missing JSON content type")
	}
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("got %s", rec.Body.String())
	}
}

func TestReadiness(t *testing.T) {
	h := NewHandler(system.NewHealthChecker(pingerFunc(func(context.Context) error { return nil })), nil, testIdentityConfig)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal("missing JSON content type")
	}
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("got %s", rec.Body.String())
	}
}

func TestReadinessHidesDependencyErrors(t *testing.T) {
	const requestID = "0123456789abcdef0123456789abcdef"
	h := NewHandler(system.NewHealthChecker(pingerFunc(func(context.Context) error {
		return errors.New("postgres password=secret")
	})), nil, testIdentityConfig)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil)
	req.Header.Set("X-Request-ID", requestID)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "postgres") || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("response leaked dependency error: %s", rec.Body.String())
	}
	var body ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "dependency_unavailable" || body.Error.Message != "Service is not ready" || body.Error.RequestId != requestID {
		t.Fatalf("unexpected error response: %+v", body)
	}
}
