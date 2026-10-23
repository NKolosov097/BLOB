package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"blob/server/internal/modules/identity"
	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
	"blob/server/internal/platform/requestid"
)

type handler struct {
	health   system.HealthChecker
	sessions *identity.Sessions
	identity config.Identity
}

func NewHandler(health system.HealthChecker, sessions *identity.Sessions, identityConfig config.Identity, middleware ...func(http.Handler) http.Handler) http.Handler {
	h := handler{health: health, sessions: sessions, identity: identityConfig}
	strict := NewStrictHandlerWithOptions(h, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeParamError,
		ResponseErrorHandlerFunc: writeDomainError,
	})
	router := HandlerWithOptions(strict, StdHTTPServerOptions{BaseURL: "/api/v1", ErrorHandlerFunc: writeParamError})
	router = identityGuard(identityConfig.PublicOrigin)(router)
	router = corsMiddleware(identityConfig.PublicOrigin)(router)
	for i := len(middleware) - 1; i >= 0; i-- {
		router = middleware[i](router)
	}
	return requestid.Middleware(router)
}

func (h handler) GetLiveness(ctx context.Context, _ GetLivenessRequestObject) (GetLivenessResponseObject, error) {
	if err := h.health.Live(ctx); err != nil {
		return nil, err
	}
	return GetLiveness200JSONResponse{Status: Ok}, nil
}

func (h handler) GetReadiness(ctx context.Context, _ GetReadinessRequestObject) (GetReadinessResponseObject, error) {
	if err := h.health.Ready(ctx); err != nil {
		var response GetReadiness503JSONResponse
		response.Error.Code = "dependency_unavailable"
		response.Error.Message = "Service is not ready"
		response.Error.RequestId = requestid.FromContext(ctx)
		return response, nil
	}
	return GetReadiness200JSONResponse{Status: Ok}, nil
}

// requestContextKey carries the raw *http.Request/http.ResponseWriter pair
// into strict handlers, which otherwise only see the typed request object.
// Only identityGuard sets this, so only identity operations use it.
type requestContextKey struct{}

type requestContextValue struct {
	r *http.Request
	w http.ResponseWriter
}

func requestContext(ctx context.Context) (*http.Request, http.ResponseWriter) {
	v, _ := ctx.Value(requestContextKey{}).(requestContextValue)
	return v.r, v.w
}

// mutatingOperations lists the four identity operations that require exact
// Origin and CSRF validation.
var mutatingOperations = map[string]bool{
	"Logout":               true,
	"RevokeSession":        true,
	"RevokeDeviceSessions": true,
	"RevokeAllSessions":    true,
}

// allowedQueryParams lists the accepted query keys for the identity GET
// operations that have one; DELETE/POST operations are not in this map and
// so are not subject to the unknown-key check.
var allowedQueryParams = map[string]map[string]bool{
	"GetAuthContext": {},
	"GetCurrentUser": {},
	"ListSessions":   {"cursor": true},
	"ListDevices":    {},
}

const maxBodyBytes = 16 * 1024
const apiBase = "/api/v1"

// classifyIdentityRequest identifies which of the eight session/device/
// context operations a raw request targets, from method and path alone, so
// Origin/body/query guards can run before the generated router parses any
// parameter (a malformed path UUID must not bypass those guards).
func classifyIdentityRequest(method, path string) (operationID string, ok bool) {
	if method == http.MethodHead {
		method = http.MethodGet
	}
	rest, ok := strings.CutPrefix(path, apiBase)
	if !ok {
		return "", false
	}
	switch {
	case method == http.MethodGet && rest == "/auth/context":
		return "GetAuthContext", true
	case method == http.MethodPost && rest == "/auth/logout":
		return "Logout", true
	case method == http.MethodGet && rest == "/identity/me":
		return "GetCurrentUser", true
	case method == http.MethodGet && rest == "/identity/sessions":
		return "ListSessions", true
	case method == http.MethodDelete && rest == "/identity/sessions":
		return "RevokeAllSessions", true
	case method == http.MethodGet && rest == "/identity/devices":
		return "ListDevices", true
	case method == http.MethodDelete && singlePathSegment(rest, "/identity/sessions/", ""):
		return "RevokeSession", true
	case method == http.MethodDelete && singlePathSegment(rest, "/identity/devices/", "/sessions"):
		return "RevokeDeviceSessions", true
	default:
		return "", false
	}
}

// singlePathSegment reports whether rest is exactly prefix, one nonempty
// path segment with no further slash, then suffix.
func singlePathSegment(rest, prefix, suffix string) bool {
	if len(rest) <= len(prefix)+len(suffix) || !strings.HasPrefix(rest, prefix) || !strings.HasSuffix(rest, suffix) {
		return false
	}
	middle := rest[len(prefix) : len(rest)-len(suffix)]
	return middle != "" && !strings.Contains(middle, "/")
}

// identityGuard enforces the shared no-store, body-size, unknown-query and
// mutation-Origin rules for the eight session/device/context operations,
// wrapping the entire router so these checks run before the generated
// parameter binding (a malformed UUID or query string must not bypass
// them), and stashes the raw request/response writer for handlers that need
// cookies and headers the strict request object does not carry.
func identityGuard(publicOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			operationID, ok := classifyIdentityRequest(r.Method, r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-store")

			if mutatingOperations[operationID] {
				if !validateOrigin(publicOrigin, r.Header.Get("Origin")) {
					writeDomainError(w, r, errOriginDenied)
					return
				}
			}
			if err := rejectOversizedOrUnexpectedBody(w, r); err != nil {
				writeDomainError(w, r, err)
				return
			}
			if allowed, ok := allowedQueryParams[operationID]; ok {
				query, err := url.ParseQuery(r.URL.RawQuery)
				if err != nil {
					writeDomainError(w, r, errInvalidRequest)
					return
				}
				for key := range query {
					if !allowed[key] {
						writeDomainError(w, r, errInvalidRequest)
						return
					}
				}
			}

			ctx := context.WithValue(r.Context(), requestContextKey{}, requestContextValue{r: r, w: w})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// rejectOversizedOrUnexpectedBody enforces the global body cap first, then
// rejects any nonempty body on these bodyless operations.
func rejectOversizedOrUnexpectedBody(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	n, err := io.CopyN(io.Discard, r.Body, maxBodyBytes+1)
	if err != nil && !errors.Is(err, io.EOF) {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errRequestTooLarge
		}
		return errInvalidRequest
	}
	if n > 0 {
		return errInvalidRequest
	}
	return nil
}

// corsMiddleware answers preflight and annotates allowed cross-origin
// responses for the one configured trusted public origin; every other
// origin receives no CORS allow headers.
func corsMiddleware(publicOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")
			if origin := r.Header.Get("Origin"); origin != "" && origin == publicOrigin {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
				w.Header().Set("Access-Control-Allow-Headers", "X-CSRF-Token")
			}
			// Answer only a genuine preflight from the trusted origin. Any
			// other OPTIONS falls through so the router reports its own
			// 404/405 instead of a blanket 204.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" &&
				r.Header.Get("Origin") == publicOrigin {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeSafeError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var response ErrorResponse
	response.Error.Code = code
	response.Error.Message = message
	response.Error.RequestId = requestid.FromContext(r.Context())
	_ = json.NewEncoder(w).Encode(response)
}

// writeParamError backs StdHTTPServerOptions.ErrorHandlerFunc, covering
// malformed path/query parameters the generated router rejects before a
// strict handler runs. A missing required CSRF header is reported as
// csrf_failed, not a generic parser error; identityGuard already took
// precedence over Origin for mutating operations before this ever runs.
func writeParamError(w http.ResponseWriter, r *http.Request, err error) {
	var headerErr *RequiredHeaderError
	if errors.As(err, &headerErr) && headerErr.ParamName == "X-CSRF-Token" {
		writeSafeError(w, r, http.StatusForbidden, "csrf_failed", "CSRF validation failed.")
		return
	}
	writeSafeError(w, r, http.StatusBadRequest, "invalid_request", "The request was invalid.")
}

// writeDomainError backs StrictHTTPServerOptions.ResponseErrorHandlerFunc,
// the single place that maps every typed domain/transport error to a safe
// envelope. It never includes err.Error() in the response.
func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errInvalidRequest):
		writeSafeError(w, r, http.StatusBadRequest, "invalid_request", "The request was invalid.")
	case errors.Is(err, errRequestTooLarge):
		writeSafeError(w, r, http.StatusRequestEntityTooLarge, "request_too_large", "The request body was too large.")
	case errors.Is(err, errOriginDenied):
		writeSafeError(w, r, http.StatusForbidden, "origin_denied", "The request origin was not allowed.")
	case errors.Is(err, errCSRFFailed):
		writeSafeError(w, r, http.StatusForbidden, "csrf_failed", "CSRF validation failed.")
	case errors.Is(err, identity.ErrUnauthenticated):
		writeSafeError(w, r, http.StatusUnauthorized, "authentication_required", "Authentication is required.")
	case errors.Is(err, identity.ErrNotFound):
		writeSafeError(w, r, http.StatusNotFound, "not_found", "The requested resource was not found.")
	case errors.Is(err, identity.ErrDeviceLimit):
		writeSafeError(w, r, http.StatusConflict, "device_limit", "The device limit has been reached.")
	case errors.Is(err, identity.ErrUnavailable):
		writeSafeError(w, r, http.StatusServiceUnavailable, "dependency_unavailable", "A required dependency is unavailable.")
	default:
		writeSafeError(w, r, http.StatusInternalServerError, "internal_error", "An unexpected error occurred.")
	}
}
