package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"blob/server/internal/modules/identity"

	"github.com/google/uuid"
)

// authFromSessionCookie resolves the Actor and raw token for the current
// request's session cookie. A missing, duplicate or malformed cookie fails
// as unauthenticated without a database lookup.
func (h handler) authFromSessionCookie(ctx context.Context, r *http.Request) (identity.Actor, string, error) {
	raw, present, duplicate := readCookie(r, cookieSession)
	if !present || duplicate {
		return identity.Actor{}, "", identity.ErrUnauthenticated
	}
	if h.sessions == nil {
		return identity.Actor{}, "", identity.ErrUnavailable
	}
	actor, err := h.sessions.Authenticate(ctx, raw)
	if err != nil {
		return identity.Actor{}, "", err
	}
	return actor, raw, nil
}

func (h handler) GetAuthContext(ctx context.Context, _ GetAuthContextRequestObject) (GetAuthContextResponseObject, error) {
	r, w := requestContext(ctx)
	now := time.Now().UTC()

	if raw, present, duplicate := readCookie(r, cookieSession); present && !duplicate {
		if h.sessions == nil {
			return nil, identity.ErrUnavailable
		}
		if _, err := h.sessions.Authenticate(ctx, raw); err == nil {
			return GetAuthContext200JSONResponse{
				CsrfToken:      csrfForSession(h.identity.BrowserKey, raw),
				EnabledMethods: []string{},
			}, nil
		} else if !errors.Is(err, identity.ErrUnauthenticated) {
			return nil, err
		}
	}

	if contextRaw, present, duplicate := readCookie(r, cookieContext); present && !duplicate &&
		verifyAnonymousContext(h.identity.BrowserKey, contextRaw, now) {
		return GetAuthContext200JSONResponse{
			CsrfToken:      csrfForContext(h.identity.BrowserKey, contextRaw),
			EnabledMethods: []string{},
		}, nil
	}

	fresh, err := newAnonymousContext(h.identity.BrowserKey, now)
	if err != nil {
		return nil, err
	}
	setCookie(w, cookieContext, fresh, contextTTL)
	return GetAuthContext200JSONResponse{
		CsrfToken:      csrfForContext(h.identity.BrowserKey, fresh),
		EnabledMethods: []string{},
	}, nil
}

func (h handler) GetCurrentUser(ctx context.Context, _ GetCurrentUserRequestObject) (GetCurrentUserResponseObject, error) {
	r, _ := requestContext(ctx)
	actor, _, err := h.authFromSessionCookie(ctx, r)
	if err != nil {
		return nil, err
	}
	return GetCurrentUser200JSONResponse{
		Id:              actor.UserID,
		AccountStatus:   CurrentUserAccountStatus(actor.AccessState),
		MfaRequired:     actor.MFARequired,
		SecurityVersion: actor.SecurityVersion,
	}, nil
}

func (h handler) ListSessions(ctx context.Context, request ListSessionsRequestObject) (ListSessionsResponseObject, error) {
	r, _ := requestContext(ctx)
	actor, _, err := h.authFromSessionCookie(ctx, r)
	if err != nil {
		return nil, err
	}
	var after *identity.SessionCursor
	if request.Params.Cursor != nil && *request.Params.Cursor != "" {
		decoded, err := decodeCursor(h.identity.BrowserKey, actor.UserID, "sessions", *request.Params.Cursor)
		if err != nil {
			return nil, err
		}
		after = &decoded
	}
	items, hasMore, err := h.sessions.ListSessions(ctx, actor.UserID, actor.SessionID, after)
	if err != nil {
		return nil, err
	}
	page := SessionPage{Items: make([]Session, 0, len(items))}
	for _, item := range items {
		page.Items = append(page.Items, Session{
			Id:                item.ID,
			DeviceId:          item.DeviceID,
			CreatedAt:         item.CreatedAt,
			LastSeenAt:        item.LastSeenAt,
			IdleExpiresAt:     item.IdleExpiresAt,
			AbsoluteExpiresAt: item.AbsoluteExpiresAt,
			Current:           item.Current,
		})
	}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		next := encodeCursor(h.identity.BrowserKey, actor.UserID, "sessions", identity.SessionCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		page.NextCursor = &next
	}
	return ListSessions200JSONResponse(page), nil
}

func (h handler) ListDevices(ctx context.Context, _ ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	r, _ := requestContext(ctx)
	actor, _, err := h.authFromSessionCookie(ctx, r)
	if err != nil {
		return nil, err
	}
	items, err := h.sessions.ListDevices(ctx, actor.UserID, actor.DeviceID)
	if err != nil {
		return nil, err
	}
	page := DevicePage{Items: make([]Device, 0, len(items))}
	for _, item := range items {
		page.Items = append(page.Items, Device{
			Id:         item.ID,
			Browser:    item.Browser,
			Os:         item.OS,
			CreatedAt:  item.CreatedAt,
			LastSeenAt: item.LastSeenAt,
			Current:    item.Current,
		})
	}
	return ListDevices200JSONResponse(page), nil
}

// Logout is the one access-reducing CSRF exception. An active session
// accepts only its own session-bound CSRF, never the anonymous fallback.
// Only once the session is confirmed inactive/missing/malformed does a
// matching old session-CSRF or a valid anonymous-context CSRF clear the
// stale cookie. A well-formed cookie whose database lookup fails returns a
// safe 503 and never clears the cookie.
func (h handler) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	r, w := requestContext(ctx)

	if raw, present, duplicate := readCookie(r, cookieSession); present && !duplicate {
		if h.sessions == nil {
			return nil, identity.ErrUnavailable
		}
		_, err := h.sessions.Authenticate(ctx, raw)
		switch {
		case err == nil:
			// Active session: only its own session-bound CSRF can clear it.
			if !verifyHMAC(csrfForSession(h.identity.BrowserKey, raw), r.Header.Get("X-CSRF-Token")) {
				return nil, errCSRFFailed
			}
			if err := h.sessions.Logout(ctx, raw); err != nil {
				return nil, err
			}
			clearCookie(w, cookieSession)
			return Logout204Response{}, nil
		case errors.Is(err, identity.ErrUnauthenticated):
			// Inactive/expired/revoked/malformed: its own derived CSRF can
			// still clear the stale cookie; otherwise fall through to the
			// anonymous-context fallback below.
			if verifyHMAC(csrfForSession(h.identity.BrowserKey, raw), r.Header.Get("X-CSRF-Token")) {
				if err := h.sessions.Logout(ctx, raw); err != nil {
					return nil, err
				}
				clearCookie(w, cookieSession)
				return Logout204Response{}, nil
			}
		default:
			return nil, err
		}
	}

	if contextRaw, present, duplicate := readCookie(r, cookieContext); present && !duplicate &&
		verifyAnonymousContext(h.identity.BrowserKey, contextRaw, time.Now().UTC()) &&
		verifyHMAC(csrfForContext(h.identity.BrowserKey, contextRaw), r.Header.Get("X-CSRF-Token")) {
		clearCookie(w, cookieSession)
		return Logout204Response{}, nil
	}

	return nil, errCSRFFailed
}

// authorizeMutation validates the session-bound CSRF proof and the active
// session in one step. Active-session mutations never accept the anonymous
// context fallback.
func (h handler) authorizeMutation(ctx context.Context, r *http.Request) (identity.Actor, error) {
	raw, present, duplicate := readCookie(r, cookieSession)
	if !present || duplicate {
		return identity.Actor{}, identity.ErrUnauthenticated
	}
	if !verifyHMAC(csrfForSession(h.identity.BrowserKey, raw), r.Header.Get("X-CSRF-Token")) {
		return identity.Actor{}, errCSRFFailed
	}
	if h.sessions == nil {
		return identity.Actor{}, identity.ErrUnavailable
	}
	return h.sessions.Authenticate(ctx, raw)
}

func (h handler) RevokeSession(ctx context.Context, request RevokeSessionRequestObject) (RevokeSessionResponseObject, error) {
	r, w := requestContext(ctx)
	actor, err := h.authorizeMutation(ctx, r)
	if err != nil {
		return nil, err
	}
	targetID := uuid.UUID(request.Id)
	if err := h.sessions.RevokeSession(ctx, actor, targetID); err != nil {
		return nil, err
	}
	if targetID == actor.SessionID {
		clearCookie(w, cookieSession)
	}
	return RevokeSession204Response{}, nil
}

func (h handler) RevokeDeviceSessions(ctx context.Context, request RevokeDeviceSessionsRequestObject) (RevokeDeviceSessionsResponseObject, error) {
	r, w := requestContext(ctx)
	actor, err := h.authorizeMutation(ctx, r)
	if err != nil {
		return nil, err
	}
	targetDeviceID := uuid.UUID(request.Id)
	if err := h.sessions.RevokeDeviceSessions(ctx, actor, targetDeviceID); err != nil {
		return nil, err
	}
	if targetDeviceID == actor.DeviceID {
		clearCookie(w, cookieSession)
	}
	return RevokeDeviceSessions204Response{}, nil
}

func (h handler) RevokeAllSessions(ctx context.Context, _ RevokeAllSessionsRequestObject) (RevokeAllSessionsResponseObject, error) {
	r, w := requestContext(ctx)
	actor, err := h.authorizeMutation(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := h.sessions.RevokeAllSessions(ctx, actor); err != nil {
		return nil, err
	}
	clearCookie(w, cookieSession)
	return RevokeAllSessions204Response{}, nil
}
