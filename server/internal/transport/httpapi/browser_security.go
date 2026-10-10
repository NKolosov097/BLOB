package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"blob/server/internal/modules/identity"

	"github.com/google/uuid"
)

const (
	cookieSession = "__Host-blob_session"
	cookieDevice  = "__Host-blob_device"
	cookieContext = "__Host-blob_context"

	contextTTL   = 24 * time.Hour
	maxCursorLen = 1024
)

var (
	errInvalidRequest  = errors.New("invalid_request")
	errRequestTooLarge = errors.New("request_too_large")
	errOriginDenied    = errors.New("origin_denied")
	errCSRFFailed      = errors.New("csrf_failed")
)

// setCookie writes one of the three browser cookies with the fixed, shared
// attributes this slice requires.
func setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(maxAge).UTC(),
		MaxAge:   int(maxAge.Seconds()),
	})
}

// clearCookie expires a cookie immediately using the same attributes it was
// set with, so the browser actually removes it.
func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(0, 0).UTC(),
		MaxAge:   -1,
	})
}

// readCookie returns the single value for name. A missing cookie is
// reported via !present; more than one same-named cookie is reported via
// duplicate and must be treated as malformed, not arbitrarily selected.
func readCookie(r *http.Request, name string) (value string, present bool, duplicate bool) {
	var found []string
	for _, c := range r.Cookies() {
		if c.Name == name {
			found = append(found, c.Value)
		}
	}
	switch len(found) {
	case 0:
		return "", false, false
	case 1:
		return found[0], true, false
	default:
		return "", true, true
	}
}

func signHMAC(key [32]byte, message string) string {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verifyHMAC(expected, got string) bool {
	if got == "" {
		return false
	}
	return hmac.Equal([]byte(expected), []byte(got))
}

// newAnonymousContext issues a stateless signed browser context cookie
// value: v1.<expires>.<random>.<hmac>.
func newAnonymousContext(key [32]byte, now time.Time) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	random := base64.RawURLEncoding.EncodeToString(buf)
	expires := strconv.FormatInt(now.Add(contextTTL).Unix(), 10)
	canonical := "blob.context.v1:" + expires + ":" + random
	mac := signHMAC(key, canonical)
	return "v1." + expires + "." + random + "." + mac, nil
}

// verifyAnonymousContext checks the signature and expiry of a context cookie
// value. It performs no database access.
func verifyAnonymousContext(key [32]byte, raw string, now time.Time) bool {
	parts := strings.Split(raw, ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return false
	}
	expires, random, mac := parts[1], parts[2], parts[3]
	expiresUnix, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return false
	}
	canonical := "blob.context.v1:" + expires + ":" + random
	if !verifyHMAC(signHMAC(key, canonical), mac) {
		return false
	}
	return now.Before(time.Unix(expiresUnix, 0))
}

// csrfForSession derives the CSRF token bound to a full raw session token.
func csrfForSession(key [32]byte, rawSessionToken string) string {
	return signHMAC(key, "blob.csrf.v1:session:"+rawSessionToken)
}

// csrfForContext derives the CSRF token bound to a validated anonymous
// browser context payload.
func csrfForContext(key [32]byte, rawContext string) string {
	return signHMAC(key, "blob.csrf.v1:context:"+rawContext)
}

// validateOrigin requires an exact, byte-for-byte match against the one
// configured trusted public origin.
func validateOrigin(configured, got string) bool {
	return got != "" && got == configured
}

type cursorPayload struct {
	V         int    `json:"v"`
	Operation string `json:"op"`
	UserID    string `json:"u"`
	CreatedAt string `json:"c"`
	ID        string `json:"i"`
}

// encodeCursor signs a keyset-pagination cursor bound to the owner and
// operation, so it cannot be replayed against another list or account.
func encodeCursor(key [32]byte, userID uuid.UUID, operation string, cursor identity.SessionCursor) string {
	payload := cursorPayload{
		V: 1, Operation: operation, UserID: userID.String(),
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID.String(),
	}
	body, _ := json.Marshal(payload)
	encodedBody := base64.RawURLEncoding.EncodeToString(body)
	return encodedBody + "." + signHMAC(key, "blob.cursor.v1:"+encodedBody)
}

// decodeCursor verifies the signature, owner binding and operation of a
// cursor token before returning its pagination position.
func decodeCursor(key [32]byte, userID uuid.UUID, operation, token string) (identity.SessionCursor, error) {
	if token == "" || len(token) > maxCursorLen {
		return identity.SessionCursor{}, errInvalidRequest
	}
	encodedBody, mac, ok := strings.Cut(token, ".")
	if !ok {
		return identity.SessionCursor{}, errInvalidRequest
	}
	if !verifyHMAC(signHMAC(key, "blob.cursor.v1:"+encodedBody), mac) {
		return identity.SessionCursor{}, errInvalidRequest
	}
	body, err := base64.RawURLEncoding.DecodeString(encodedBody)
	if err != nil {
		return identity.SessionCursor{}, errInvalidRequest
	}
	var payload cursorPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return identity.SessionCursor{}, errInvalidRequest
	}
	if payload.V != 1 || payload.Operation != operation || payload.UserID != userID.String() {
		return identity.SessionCursor{}, errInvalidRequest
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return identity.SessionCursor{}, errInvalidRequest
	}
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return identity.SessionCursor{}, errInvalidRequest
	}
	return identity.SessionCursor{CreatedAt: createdAt, ID: id}, nil
}
