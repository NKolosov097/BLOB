package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"blob/server/internal/modules/identity"

	"github.com/google/uuid"
)

var testKey = [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
var otherKey = [32]byte{9, 9, 9}

func TestSetCookieHasRequiredAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	setCookie(rec, cookieSession, "raw-value", time.Hour)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies", len(cookies))
	}
	c := cookies[0]
	if c.Name != cookieSession || c.Value != "raw-value" {
		t.Fatalf("unexpected cookie: %+v", c)
	}
	if !c.Secure || !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("missing required attributes: %+v", c)
	}
	if c.Domain != "" {
		t.Fatalf("cookie must not set Domain: %+v", c)
	}
}

func TestClearCookieExpiresInThePast(t *testing.T) {
	rec := httptest.NewRecorder()
	clearCookie(rec, cookieSession)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("got %d cookies", len(cookies))
	}
	c := cookies[0]
	if c.Value != "" || c.MaxAge != -1 {
		t.Fatalf("clear cookie did not expire: %+v", c)
	}
	if !c.Secure || !c.HttpOnly || c.Path != "/" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("clear cookie dropped required attributes: %+v", c)
	}
	if c.Expires.After(time.Now()) {
		t.Fatalf("clear cookie expiry is not in the past: %v", c.Expires)
	}
}

func TestReadCookieDetectsMissingSingleAndDuplicate(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, present, duplicate := readCookie(req, cookieSession); present || duplicate {
		t.Fatal("missing cookie misreported")
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieSession, Value: "a"})
	if value, present, duplicate := readCookie(req, cookieSession); !present || duplicate || value != "a" {
		t.Fatalf("single cookie misreported: value=%q present=%t duplicate=%t", value, present, duplicate)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieSession, Value: "a"})
	req.AddCookie(&http.Cookie{Name: cookieSession, Value: "b"})
	if value, present, duplicate := readCookie(req, cookieSession); !present || !duplicate || value != "" {
		t.Fatalf("duplicate cookie not rejected as malformed: value=%q present=%t duplicate=%t", value, present, duplicate)
	}
}

func TestAnonymousContextRoundTripsAndExpires(t *testing.T) {
	now := time.Now().UTC()
	context, err := newAnonymousContext(testKey, now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(context, "v1.") {
		t.Fatalf("unexpected context format: %q", context)
	}
	if !verifyAnonymousContext(testKey, context, now) {
		t.Fatal("freshly issued context did not verify")
	}
	if verifyAnonymousContext(testKey, context, now.Add(24*time.Hour+time.Second)) {
		t.Fatal("expired context still verified")
	}
	if verifyAnonymousContext(otherKey, context, now) {
		t.Fatal("context verified under the wrong key")
	}
	if verifyAnonymousContext(testKey, context+"tampered", now) {
		t.Fatal("tampered context still verified")
	}
	if verifyAnonymousContext(testKey, "not.a.valid.context", now) {
		t.Fatal("malformed context verified")
	}
}

func TestCSRFIsBoundToExactSessionOrContext(t *testing.T) {
	sessionToken := "raw-session-token"
	csrf := csrfForSession(testKey, sessionToken)
	if !verifyHMAC(csrf, csrf) {
		t.Fatal("CSRF did not verify against itself")
	}
	if verifyHMAC(csrfForSession(testKey, "other-token"), csrf) {
		t.Fatal("CSRF verified for a different session token")
	}
	if verifyHMAC(csrfForSession(otherKey, sessionToken), csrf) {
		t.Fatal("CSRF verified under the wrong key")
	}

	contextPayload := "v1.123.random.mac"
	contextCSRF := csrfForContext(testKey, contextPayload)
	if verifyHMAC(contextCSRF, csrf) {
		t.Fatal("session and context CSRF domains were not separated")
	}
	if verifyHMAC("", "") {
		t.Fatal("empty CSRF must never verify")
	}
}

func TestValidateOriginIsByteForByteExact(t *testing.T) {
	const configured = "https://app.example.test"
	cases := []struct {
		got  string
		want bool
	}{
		{"https://app.example.test", true},
		{"", false},
		{"http://app.example.test", false},
		{"https://sub.app.example.test", false},
		{"https://app.example.test.evil.test", false},
		{"https://app.example.test/", false},
		{"null", false},
	}
	for _, c := range cases {
		if got := validateOrigin(configured, c.got); got != c.want {
			t.Fatalf("validateOrigin(%q) = %t, want %t", c.got, got, c.want)
		}
	}
}

func TestCursorRoundTripsAndRejectsTamperOwnerAndOperationMismatch(t *testing.T) {
	userID := uuid.Must(uuid.NewV7())
	otherUser := uuid.Must(uuid.NewV7())
	cursor := identity.SessionCursor{CreatedAt: time.Now().UTC().Truncate(time.Microsecond), ID: uuid.Must(uuid.NewV7())}

	token := encodeCursor(testKey, userID, "sessions", cursor)
	decoded, err := decodeCursor(testKey, userID, "sessions", token)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.Equal(cursor.CreatedAt) || decoded.ID != cursor.ID {
		t.Fatalf("got %+v, want %+v", decoded, cursor)
	}

	if _, err := decodeCursor(testKey, otherUser, "sessions", token); err == nil {
		t.Fatal("cursor accepted for the wrong owner")
	}
	if _, err := decodeCursor(testKey, userID, "devices", token); err == nil {
		t.Fatal("cursor accepted for the wrong operation")
	}
	if _, err := decodeCursor(otherKey, userID, "sessions", token); err == nil {
		t.Fatal("cursor accepted under the wrong key")
	}
	if _, err := decodeCursor(testKey, userID, "sessions", token+"x"); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	if _, err := decodeCursor(testKey, userID, "sessions", ""); err == nil {
		t.Fatal("empty cursor accepted")
	}
	if _, err := decodeCursor(testKey, userID, "sessions", strings.Repeat("a", maxCursorLen+1)); err == nil {
		t.Fatal("oversized cursor accepted")
	}
}
