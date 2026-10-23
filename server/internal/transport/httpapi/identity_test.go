package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"blob/server/internal/modules/identity"
	"blob/server/internal/modules/system"
	"blob/server/internal/platform/config"
	"blob/server/internal/platform/database"
	"blob/server/internal/testdb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const testPublicOrigin = "https://app.example.test"

func identityTestConfig() config.Identity {
	return config.Identity{PublicOrigin: testPublicOrigin, BrowserKey: testKey}
}

func newIdentityTestServer(t *testing.T) (*httptest.Server, *identity.Sessions, *pgxpool.Pool) {
	t.Helper()
	pool, err := database.Open(t.Context(), testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sessions := identity.NewSessions(pool)
	handler := NewHandler(system.NewHealthChecker(nil), sessions, identityTestConfig())
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return server, sessions, pool
}

// newTransportTestServer serves the tests that exercise routing, CORS and
// guard behavior only. They never reach the session service, so they must not
// require a database to run locally.
func newTransportTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(NewHandler(system.NewHealthChecker(nil), nil, identityTestConfig()))
	t.Cleanup(server.Close)
	return server
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func createTestUser(t *testing.T, pool *pgxpool.Pool, accessState string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `INSERT INTO users (id, access_state) VALUES ($1, $2)`, toPgUUID(id), accessState); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM sessions WHERE user_id=$1`, toPgUUID(id))
		_, _ = pool.Exec(ctx, `DELETE FROM devices WHERE user_id=$1`, toPgUUID(id))
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, toPgUUID(id))
	})
	return id
}

type testCase struct {
	t      *testing.T
	server *httptest.Server
}

func (c testCase) request(method, path string) *http.Request {
	req, err := http.NewRequest(method, c.server.URL+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	return req
}

func (c testCase) do(req *http.Request) *http.Response {
	c.t.Helper()
	resp, err := c.server.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	// Every contract response carries no-store, errors included. Native
	// router 404/405 replies are text/plain and are outside the contract.
	contract := resp.StatusCode == http.StatusNoContent ||
		strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json")
	if contract && req.Method != http.MethodOptions && (strings.Contains(req.URL.Path, "/auth/") || strings.Contains(req.URL.Path, "/identity/")) && resp.Header.Get("Cache-Control") != "no-store" {
		c.t.Fatal("identity response missing no-store")
	}
	c.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func decodeError(t *testing.T, resp *http.Response) ErrorResponse {
	t.Helper()
	var body ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

func sessionCookie(token string) *http.Cookie {
	return &http.Cookie{Name: cookieSession, Value: token}
}

func TestHealthEndpointsIgnoreIdentityGuard(t *testing.T) {
	handler := NewHandler(system.NewHealthChecker(nil), nil, identityTestConfig())
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	c := testCase{t: t, server: server}

	resp := c.do(c.request(http.MethodGet, "/api/v1/health/live"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "" {
		t.Fatal("health endpoint unexpectedly gained no-store")
	}
}

func TestGetAuthContextAnonymousIssuesContextCookie(t *testing.T) {
	server, _, _ := newIdentityTestServer(t)
	c := testCase{t: t, server: server}

	resp := c.do(c.request(http.MethodGet, "/api/v1/auth/context"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	var found *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == cookieContext {
			found = ck
		}
	}
	if found == nil {
		t.Fatal("anonymous context cookie not issued")
	}
	var body AuthContext
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.CsrfToken == "" || len(body.EnabledMethods) != 0 {
		t.Fatalf("unexpected context body: %+v", body)
	}
	if !verifyHMAC(csrfForContext(testKey, found.Value), body.CsrfToken) {
		t.Fatal("returned CSRF token does not match the issued context cookie")
	}
}

func TestGetAuthContextReusesValidContextCookie(t *testing.T) {
	server, _, _ := newIdentityTestServer(t)
	c := testCase{t: t, server: server}

	fresh, err := newAnonymousContext(testKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	req := c.request(http.MethodGet, "/api/v1/auth/context")
	req.AddCookie(&http.Cookie{Name: cookieContext, Value: fresh})
	resp := c.do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == cookieContext {
			t.Fatal("a still-valid context cookie must not be replaced")
		}
	}
	var body AuthContext
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !verifyHMAC(csrfForContext(testKey, fresh), body.CsrfToken) {
		t.Fatal("CSRF token not derived from the reused context")
	}
}

func TestGetAuthContextSessionBound(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodGet, "/api/v1/auth/context")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body AuthContext
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !verifyHMAC(csrfForSession(testKey, issued.SessionToken), body.CsrfToken) {
		t.Fatal("CSRF token not derived from the active session")
	}
}

func TestGetAuthContextDatabaseOutageReturns503(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	c := testCase{t: t, server: server}

	req := c.request(http.MethodGet, "/api/v1/auth/context")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 for a well-formed session during an outage", resp.StatusCode)
	}
	if len(resp.Cookies()) != 0 {
		t.Fatal("outage response must not silently fall back to anonymous and set a cookie")
	}
	body := decodeError(t, resp)
	if body.Error.Code != "dependency_unavailable" {
		t.Fatalf("got %q", body.Error.Code)
	}
}

func TestIdentityReadsRequireAuthentication(t *testing.T) {
	server, _, _ := newIdentityTestServer(t)
	c := testCase{t: t, server: server}

	for _, path := range []string{"/api/v1/identity/me", "/api/v1/identity/sessions", "/api/v1/identity/devices"} {
		resp := c.do(c.request(http.MethodGet, path))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: got %d", path, resp.StatusCode)
		}
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: missing no-store", path)
		}
		body := decodeError(t, resp)
		if body.Error.Code != "authentication_required" {
			t.Fatalf("%s: got %q", path, body.Error.Code)
		}
	}
}

func TestGetCurrentUserReturnsActor(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodGet, "/api/v1/identity/me")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var body CurrentUser
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Id != userID || body.AccountStatus != "active" || body.SecurityVersion != 1 || body.MfaRequired {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestListSessionsUnknownQueryParamRejected(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodGet, "/api/v1/identity/sessions?unexpected=1")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if decodeError(t, resp).Error.Code != "invalid_request" {
		t.Fatal("unexpected error code")
	}
}

func TestListSessionsMalformedQueryEncodingRejected(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodGet, "/api/v1/auth/context?unknown=%zz")
	resp := c.do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d for malformed percent-encoding", resp.StatusCode)
	}

	req = c.request(http.MethodGet, "/api/v1/identity/sessions?cursor=valid-shape")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp = c.do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d for a malformed cursor", resp.StatusCode)
	}
}

func TestListSessionsPaginatesWithSignedCursor(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	var token, deviceToken string
	var currentID uuid.UUID
	expected := map[uuid.UUID]bool{}
	for i := 0; i < 55; i++ {
		issued, err := sessions.Issue(t.Context(), userID, deviceToken, identity.BrowserChrome, identity.OSWindows)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			deviceToken = issued.DeviceToken
		}
		token, currentID = issued.SessionToken, issued.SessionID
		expected[issued.SessionID] = true
	}
	c := testCase{t: t, server: server}
	seen := map[uuid.UUID]bool{}
	path := "/api/v1/identity/sessions"
	for pageNumber, count := range []int{50, 5} {
		req := c.request(http.MethodGet, path)
		req.AddCookie(sessionCookie(token))
		resp := c.do(req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("got %d", resp.StatusCode)
		}
		var page SessionPage
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != count {
			t.Fatalf("page %d: got %d items", pageNumber, len(page.Items))
		}
		for _, item := range page.Items {
			if !expected[item.Id] || seen[item.Id] {
				t.Fatalf("unexpected or duplicate session %s", item.Id)
			}
			seen[item.Id] = true
			if item.Current != (item.Id == currentID) {
				t.Fatalf("wrong current flag for %s", item.Id)
			}
		}
		if pageNumber == 0 {
			if page.NextCursor == nil || *page.NextCursor == "" {
				t.Fatal("missing continuation")
			}
			path += "?cursor=" + *page.NextCursor
		} else if page.NextCursor != nil {
			t.Fatal("unexpected continuation")
		}
	}
	if len(seen) != len(expected) {
		t.Fatal("pagination omitted sessions")
	}
}

func TestListSessionsRejectsForeignAndTamperedCursor(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userA := createTestUser(t, pool, "active")
	userB := createTestUser(t, pool, "active")
	issuedA, err := sessions.Issue(t.Context(), userA, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	issuedB, err := sessions.Issue(t.Context(), userB, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	foreignCursor := encodeCursor(testKey, userA, "sessions", identity.SessionCursor{CreatedAt: time.Now(), ID: uuid.Must(uuid.NewV7())})
	req := c.request(http.MethodGet, "/api/v1/identity/sessions?cursor="+foreignCursor)
	req.AddCookie(sessionCookie(issuedB.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for a cursor owned by another user", resp.StatusCode)
	}

	req = c.request(http.MethodGet, "/api/v1/identity/sessions?cursor=tampered.value")
	req.AddCookie(sessionCookie(issuedA.SessionToken))
	resp = c.do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for a tampered cursor", resp.StatusCode)
	}
}

func TestListDevices(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserFirefox, identity.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodGet, "/api/v1/identity/devices")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var page DevicePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Browser != "Firefox" || page.Items[0].Os != "Linux" || !page.Items[0].Current {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func csrfHeader(req *http.Request, value string) {
	req.Header.Set("X-CSRF-Token", value)
}

func withOrigin(req *http.Request, origin string) {
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
}

func TestMutationsRejectMissingOrForeignOrigin(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	for _, origin := range []string{"", "https://evil.test", "null", "https://sub." + strings.TrimPrefix(testPublicOrigin, "https://")} {
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, origin)
		req.AddCookie(sessionCookie(issued.SessionToken))
		csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
		resp := c.do(req)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: got %d", origin, resp.StatusCode)
		}
		if decodeError(t, resp).Error.Code != "origin_denied" {
			t.Fatalf("origin %q: unexpected error code", origin)
		}
	}
}

func TestOriginCheckPrecedesMalformedUUIDAndBodyGuards(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	// Foreign Origin + malformed UUID: Origin is checked first.
	req := c.request(http.MethodDelete, "/api/v1/identity/sessions/not-a-uuid")
	withOrigin(req, "https://evil.test")
	req.AddCookie(sessionCookie(issued.SessionToken))
	csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusForbidden || decodeError(t, resp).Error.Code != "origin_denied" {
		t.Fatalf("got status=%d", resp.StatusCode)
	}

	// Allowed Origin + malformed UUID + oversized body: the cap takes
	// precedence over the generic parameter error.
	req = c.request(http.MethodDelete, "/api/v1/identity/sessions/not-a-uuid")
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(issued.SessionToken))
	csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
	req.Body = io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), 16*1024+1)))
	resp = c.do(req)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", resp.StatusCode)
	}

	// Allowed Origin + malformed UUID + empty body: a safe 400 envelope.
	req = c.request(http.MethodDelete, "/api/v1/identity/sessions/not-a-uuid")
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(issued.SessionToken))
	csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
	resp = c.do(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", resp.StatusCode)
	}
	body := decodeError(t, resp)
	if body.Error.Code != "invalid_request" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected envelope: %+v", body)
	}
}

func TestMutationsRequireCSRFHeaderAsForbiddenNotGenericBadRequest(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodPost, "/api/v1/auth/logout")
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d, want 403 for a missing CSRF header", resp.StatusCode)
	}
	if decodeError(t, resp).Error.Code != "csrf_failed" {
		t.Fatal("missing CSRF header did not map to csrf_failed")
	}

	// Foreign Origin still takes precedence over a missing CSRF header.
	req = c.request(http.MethodPost, "/api/v1/auth/logout")
	withOrigin(req, "https://evil.test")
	req.AddCookie(sessionCookie(issued.SessionToken))
	resp = c.do(req)
	if decodeError(t, resp).Error.Code != "origin_denied" {
		t.Fatal("origin check did not take precedence over missing CSRF")
	}
}

func TestMutationsRejectWrongCSRFToken(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	req := c.request(http.MethodPost, "/api/v1/auth/logout")
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(issued.SessionToken))
	csrfHeader(req, "wrong-token")
	resp := c.do(req)
	if resp.StatusCode != http.StatusForbidden || decodeError(t, resp).Error.Code != "csrf_failed" {
		t.Fatalf("got status=%d", resp.StatusCode)
	}
}

func TestRevokeSessionOwnershipAndCurrentCookieClearing(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userA := createTestUser(t, pool, "active")
	userB := createTestUser(t, pool, "active")
	issuedA, err := sessions.Issue(t.Context(), userA, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	issuedB, err := sessions.Issue(t.Context(), userB, "", identity.BrowserChrome, identity.OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	c := testCase{t: t, server: server}

	// Nonowned target: 404, no state change.
	req := c.request(http.MethodDelete, "/api/v1/identity/sessions/"+issuedB.SessionID.String())
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(issuedA.SessionToken))
	csrfHeader(req, csrfForSession(testKey, issuedA.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got %d", resp.StatusCode)
	}

	// Owned, current target: 204 and the session cookie is cleared.
	req = c.request(http.MethodDelete, "/api/v1/identity/sessions/"+issuedA.SessionID.String())
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(issuedA.SessionToken))
	csrfHeader(req, csrfForSession(testKey, issuedA.SessionToken))
	resp = c.do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d", resp.StatusCode)
	}
	var cleared bool
	for _, ck := range resp.Cookies() {
		if ck.Name == cookieSession && ck.MaxAge == -1 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("current session cookie was not cleared")
	}
}

func TestLogoutMatrix(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	userID := createTestUser(t, pool, "active")
	c := testCase{t: t, server: server}

	t.Run("active session with anonymous CSRF is forbidden and does not clear or revoke", func(t *testing.T) {
		issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
		if err != nil {
			t.Fatal(err)
		}
		anonCtx, err := newAnonymousContext(testKey, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		req.AddCookie(sessionCookie(issued.SessionToken))
		req.AddCookie(&http.Cookie{Name: cookieContext, Value: anonCtx})
		csrfHeader(req, csrfForContext(testKey, anonCtx))
		resp := c.do(req)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("got %d", resp.StatusCode)
		}
		if len(resp.Cookies()) != 0 {
			t.Fatal("rejected logout must not clear any cookie")
		}
		if _, err := sessions.Authenticate(t.Context(), issued.SessionToken); err != nil {
			t.Fatal("active session was revoked despite a rejected logout")
		}
	})

	t.Run("active session with its own CSRF clears and revokes", func(t *testing.T) {
		issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
		if err != nil {
			t.Fatal(err)
		}
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		req.AddCookie(sessionCookie(issued.SessionToken))
		csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
		resp := c.do(req)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("got %d", resp.StatusCode)
		}
		if _, err := sessions.Authenticate(t.Context(), issued.SessionToken); err == nil {
			t.Fatal("session remained active after logout")
		}
	})

	t.Run("already revoked session with its own CSRF still clears", func(t *testing.T) {
		issued, err := sessions.Issue(t.Context(), userID, "", identity.BrowserChrome, identity.OSWindows)
		if err != nil {
			t.Fatal(err)
		}
		if err := sessions.Logout(t.Context(), issued.SessionToken); err != nil {
			t.Fatal(err)
		}
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		req.AddCookie(sessionCookie(issued.SessionToken))
		csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
		resp := c.do(req)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("got %d", resp.StatusCode)
		}

		// Repeating logout again must still succeed (idempotent).
		req = c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		req.AddCookie(sessionCookie(issued.SessionToken))
		csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
		resp = c.do(req)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("repeat logout got %d", resp.StatusCode)
		}
	})

	t.Run("no cookie with valid anonymous context clears", func(t *testing.T) {
		anonCtx, err := newAnonymousContext(testKey, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		req.AddCookie(&http.Cookie{Name: cookieContext, Value: anonCtx})
		csrfHeader(req, csrfForContext(testKey, anonCtx))
		resp := c.do(req)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("got %d", resp.StatusCode)
		}
	})

	t.Run("no cookie and no CSRF is forbidden", func(t *testing.T) {
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		resp := c.do(req)
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("got %d", resp.StatusCode)
		}
	})
}

func TestNilSessionServiceReturnsSafeUnavailableNotPanic(t *testing.T) {
	handler := NewHandler(system.NewHealthChecker(nil), nil, identityTestConfig())
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	c := testCase{t: t, server: server}

	// Anonymous context has no session dependency and must still work.
	resp := c.do(c.request(http.MethodGet, "/api/v1/auth/context"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous context got %d with a nil session service", resp.StatusCode)
	}

	req := c.request(http.MethodGet, "/api/v1/identity/me")
	req.AddCookie(sessionCookie("whatever-looks-like-a-cookie"))
	resp = c.do(req)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 instead of a panic", resp.StatusCode)
	}

	req = c.request(http.MethodPost, "/api/v1/auth/logout")
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie("whatever-looks-like-a-cookie"))
	csrfHeader(req, csrfForSession(testKey, "whatever-looks-like-a-cookie"))
	resp = c.do(req)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("logout with nil session service got %d", resp.StatusCode)
	}
}

func TestCORSPreflightAndForeignOriginHeaders(t *testing.T) {
	server := newTransportTestServer(t)
	c := testCase{t: t, server: server}

	req := c.request(http.MethodOptions, "/api/v1/identity/sessions")
	withOrigin(req, testPublicOrigin)
	req.Header.Set("Access-Control-Request-Method", "DELETE")
	resp := c.do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != testPublicOrigin {
		t.Fatal("preflight missing allow-origin for the trusted origin")
	}
	if resp.Header.Get("Vary") != "Origin" {
		t.Fatal("missing Vary: Origin")
	}

	req = c.request(http.MethodGet, "/api/v1/auth/context")
	withOrigin(req, "https://evil.test")
	resp = c.do(req)
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin must receive no CORS allow headers")
	}
}

func requireClearedSession(t *testing.T, resp *http.Response) {
	t.Helper()
	for _, cookie := range resp.Cookies() {
		if cookie.Name == cookieSession && cookie.MaxAge == -1 && cookie.Expires.Before(time.Now()) && cookie.Secure && cookie.HttpOnly && cookie.Path == "/" {
			return
		}
	}
	t.Fatal("missing correctly expired session cookie")
}

func TestDeviceAndAllRevocationRoutes(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "device", true: "all"}[all], func(t *testing.T) {
			server, sessions, pool := newIdentityTestServer(t)
			user := createTestUser(t, pool, "active")
			foreign := createTestUser(t, pool, "active")
			first, err := sessions.Issue(t.Context(), user, "", identity.BrowserChrome, identity.OSWindows)
			if err != nil {
				t.Fatal(err)
			}
			deviceToken := first.DeviceToken
			if all {
				deviceToken = ""
			}
			second, err := sessions.Issue(t.Context(), user, deviceToken, identity.BrowserChrome, identity.OSWindows)
			if err != nil {
				t.Fatal(err)
			}
			other, err := sessions.Issue(t.Context(), foreign, "", identity.BrowserChrome, identity.OSWindows)
			if err != nil {
				t.Fatal(err)
			}
			c := testCase{t: t, server: server}
			mutate := func(path string) *http.Response {
				req := c.request(http.MethodDelete, path)
				withOrigin(req, testPublicOrigin)
				req.AddCookie(sessionCookie(first.SessionToken))
				csrfHeader(req, csrfForSession(testKey, first.SessionToken))
				return c.do(req)
			}
			if !all {
				resp := mutate("/api/v1/identity/devices/" + other.DeviceID.String() + "/sessions")
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("foreign device got %d", resp.StatusCode)
				}
				if _, err := sessions.Authenticate(t.Context(), other.SessionToken); err != nil {
					t.Fatal(err)
				}
			}
			path := "/api/v1/identity/devices/" + first.DeviceID.String() + "/sessions"
			if all {
				path = "/api/v1/identity/sessions"
			}
			resp := mutate(path)
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("got %d", resp.StatusCode)
			}
			requireClearedSession(t, resp)
			for _, token := range []string{first.SessionToken, second.SessionToken} {
				if _, err := sessions.Authenticate(t.Context(), token); !errors.Is(err, identity.ErrUnauthenticated) {
					t.Fatalf("revoked token: %v", err)
				}
			}
		})
	}
}

func TestIdentityHEADAndOverlappingPaths(t *testing.T) {
	server := newTransportTestServer(t)
	c := testCase{t: t, server: server}
	for _, path := range []string{"/auth/context", "/identity/me", "/identity/sessions", "/identity/devices"} {
		resp := c.do(c.request(http.MethodHead, "/api/v1"+path))
		want := http.StatusUnauthorized
		if path == "/auth/context" {
			want = http.StatusOK
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil || len(body) != 0 || resp.StatusCode != want || resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("HEAD %s: status=%d body=%q err=%v", path, resp.StatusCode, body, err)
		}
	}
	for _, path := range []string{"/identity/devices/sessions", "/identity/devices//sessions"} {
		resp := c.do(c.request(http.MethodDelete, "/api/v1"+path))
		if resp.StatusCode >= 500 {
			t.Fatalf("malformed path %s returned %d", path, resp.StatusCode)
		}
	}
}

func TestCookieAndCSRFLifetimeMatrix(t *testing.T) {
	server, sessions, pool := newIdentityTestServer(t)
	c := testCase{t: t, server: server}
	user := createTestUser(t, pool, "active")
	issue := func() identity.IssueResult {
		issued, err := sessions.Issue(t.Context(), user, "", identity.BrowserChrome, identity.OSWindows)
		if err != nil {
			t.Fatal(err)
		}
		return issued
	}
	for _, anonymous := range []bool{false, true} {
		issued := issue()
		if _, err := pool.Exec(t.Context(), `UPDATE sessions SET idle_expires_at=now()-interval '1 second' WHERE id=$1`, toPgUUID(issued.SessionID)); err != nil {
			t.Fatal(err)
		}
		req := c.request(http.MethodPost, "/api/v1/auth/logout")
		withOrigin(req, testPublicOrigin)
		req.AddCookie(sessionCookie(issued.SessionToken))
		csrf := csrfForSession(testKey, issued.SessionToken)
		if anonymous {
			raw, err := newAnonymousContext(testKey, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(&http.Cookie{Name: cookieContext, Value: raw})
			csrf = csrfForContext(testKey, raw)
		}
		csrfHeader(req, csrf)
		resp := c.do(req)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("expired session anonymous=%t: %d", anonymous, resp.StatusCode)
		}
		requireClearedSession(t, resp)
	}
	issued := issue()
	rotated, err := sessions.Rotate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	req := c.request(http.MethodPost, "/api/v1/auth/logout")
	withOrigin(req, testPublicOrigin)
	req.AddCookie(sessionCookie(rotated.SessionToken))
	csrfHeader(req, csrfForSession(testKey, issued.SessionToken))
	resp := c.do(req)
	if resp.StatusCode != http.StatusForbidden || decodeError(t, resp).Error.Code != "csrf_failed" || len(resp.Cookies()) != 0 {
		t.Fatal("previous CSRF accepted for rotated cookie or cleared cookie")
	}
	if _, err := sessions.Authenticate(t.Context(), rotated.SessionToken); err != nil {
		t.Fatal(err)
	}
	req = c.request(http.MethodGet, "/api/v1/identity/me")
	req.AddCookie(sessionCookie(rotated.SessionToken))
	req.AddCookie(sessionCookie(rotated.SessionToken))
	resp = c.do(req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("duplicate session cookies got %d", resp.StatusCode)
	}
	for _, duplicate := range []bool{false, true} {
		when := time.Now().Add(-contextTTL - time.Hour)
		if duplicate {
			when = time.Now()
		}
		raw, err := newAnonymousContext(testKey, when)
		if err != nil {
			t.Fatal(err)
		}
		req = c.request(http.MethodGet, "/api/v1/auth/context")
		req.AddCookie(&http.Cookie{Name: cookieContext, Value: raw})
		if duplicate {
			req.AddCookie(&http.Cookie{Name: cookieContext, Value: raw})
		}
		resp = c.do(req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("context replacement got %d", resp.StatusCode)
		}
		var replacement string
		for _, cookie := range resp.Cookies() {
			if cookie.Name == cookieContext {
				replacement = cookie.Value
			}
		}
		if replacement == raw || !verifyAnonymousContext(testKey, replacement, time.Now()) {
			t.Fatal("invalid fresh context replacement")
		}
		var body AuthContext
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.CsrfToken != csrfForContext(testKey, replacement) {
			t.Fatal("replacement CSRF mismatch")
		}
		if !duplicate {
			req = c.request(http.MethodPost, "/api/v1/auth/logout")
			withOrigin(req, testPublicOrigin)
			req.AddCookie(&http.Cookie{Name: cookieContext, Value: raw})
			csrfHeader(req, csrfForContext(testKey, raw))
			resp = c.do(req)
			if resp.StatusCode != http.StatusForbidden || len(resp.Cookies()) != 0 {
				t.Fatal("expired context authorized logout")
			}
		}
	}
}
