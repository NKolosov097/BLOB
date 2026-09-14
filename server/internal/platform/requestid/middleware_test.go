package requestid

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestMiddlewareReplacesUntrustedRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "../../secret")
	rec := httptest.NewRecorder()

	Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, FromContext(r.Context()))
	})).ServeHTTP(rec, req)

	id := rec.Header().Get("X-Request-ID")
	if ok, _ := regexp.MatchString(`^[a-f0-9]{32}$`, id); !ok {
		t.Fatalf("unsafe id %q", id)
	}
	if rec.Body.String() != id {
		t.Fatalf("context id %q differs from response id %q", rec.Body.String(), id)
	}
}

func TestMiddlewarePreservesTrustedRequestID(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", id)
	rec := httptest.NewRecorder()

	Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, FromContext(r.Context()))
	})).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != id {
		t.Fatalf("got response id %q", got)
	}
	if got := rec.Body.String(); got != id {
		t.Fatalf("got context id %q", got)
	}
}
