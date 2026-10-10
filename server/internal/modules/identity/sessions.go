// Package identity implements server-side session issuance, authentication
// and owner-controlled session/device management.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"blob/server/internal/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fixed session lifetimes. These are shared constants, not runtime
// configuration: a later increment may revisit them, but this slice does
// not expose them as environment-tunable options.
const (
	IdleSessionTTL     = 7 * 24 * time.Hour
	AbsoluteSessionTTL = 30 * 24 * time.Hour
	DeviceCookieTTL    = 90 * 24 * time.Hour

	maxActiveDevices = 100
	sessionPageSize  = 50
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrNotFound        = errors.New("not_found")
	ErrDeviceLimit     = errors.New("device_limit")
	ErrUnavailable     = errors.New("dependency_unavailable")
)

// Browser and OS are coarse, allowlisted device labels. Any other value is
// reduced to Other before it reaches storage.
type Browser string

const (
	BrowserChrome  Browser = "Chrome"
	BrowserEdge    Browser = "Edge"
	BrowserFirefox Browser = "Firefox"
	BrowserSafari  Browser = "Safari"
	BrowserOther   Browser = "Other"
)

type OS string

const (
	OSWindows OS = "Windows"
	OSMacOS   OS = "macOS"
	OSLinux   OS = "Linux"
	OSAndroid OS = "Android"
	OSiOS     OS = "iOS"
	OSOther   OS = "Other"
)

func normalizeBrowser(b Browser) string {
	switch b {
	case BrowserChrome, BrowserEdge, BrowserFirefox, BrowserSafari:
		return string(b)
	default:
		return string(BrowserOther)
	}
}

func normalizeOS(o OS) string {
	switch o {
	case OSWindows, OSMacOS, OSLinux, OSAndroid, OSiOS:
		return string(o)
	default:
		return string(OSOther)
	}
}

// Actor is the authenticated identity behind a request. tokenHash is kept
// for server-internal recheck-under-lock use only; it is unexported and
// never reachable from the transport layer or any API DTO.
type Actor struct {
	UserID          uuid.UUID
	SessionID       uuid.UUID
	DeviceID        uuid.UUID
	AccessState     string
	MFARequired     bool
	SecurityVersion int64
	tokenHash       [32]byte
}

type SessionSummary struct {
	ID                uuid.UUID
	DeviceID          uuid.UUID
	CreatedAt         time.Time
	LastSeenAt        time.Time
	IdleExpiresAt     time.Time
	AbsoluteExpiresAt time.Time
	Current           bool
}

type DeviceSummary struct {
	ID         uuid.UUID
	Browser    string
	OS         string
	CreatedAt  time.Time
	LastSeenAt time.Time
	Current    bool
}

// SessionCursor identifies the last row of a page for keyset pagination.
type SessionCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

type IssueResult struct {
	SessionToken string
	// DeviceToken is empty when an existing device association was reused.
	DeviceToken       string
	SessionID         uuid.UUID
	DeviceID          uuid.UUID
	AbsoluteExpiresAt time.Time
}

type RotateResult struct {
	SessionToken      string
	SessionID         uuid.UUID
	AbsoluteExpiresAt time.Time
}

// Sessions is the concrete session service over pgxpool.
type Sessions struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewSessions(pool *pgxpool.Pool) *Sessions {
	return &Sessions{pool: pool, now: time.Now}
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	return id.Bytes
}

func toPgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t.UTC(), Valid: true}
}

var farFutureTimestamp = pgtype.Timestamptz{Time: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC), Valid: true}

var maxUUID = pgtype.UUID{
	Bytes: [16]byte{
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
	},
	Valid: true,
}

func unavailable(err error) error {
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// internalFailure wraps a server-local failure (random/ID generation) that
// is not a dependency outage; it deliberately does not match ErrUnavailable
// so the transport layer maps it to the generic 500 internal_error code.
func internalFailure(err error) error {
	return fmt.Errorf("identity: %w", err)
}

func newRawToken() (raw string, hash [32]byte, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", hash, err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	hash = sha256.Sum256(buf)
	return raw, hash, nil
}

// decodeToken fails closed on any malformed input, without a database
// lookup.
func decodeToken(raw string) (hash [32]byte, ok bool) {
	if raw == "" || len(raw) > 64 {
		return hash, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return hash, false
	}
	return sha256.Sum256(decoded), true
}

// Authenticate validates a raw session cookie token and atomically refreshes
// idle expiry. It requires an active account and a nonrevoked device.
func (s *Sessions) Authenticate(ctx context.Context, rawToken string) (Actor, error) {
	hash, ok := decodeToken(rawToken)
	if !ok {
		return Actor{}, ErrUnauthenticated
	}
	now := s.now().UTC()
	row, err := db.New(s.pool).AuthenticateSession(ctx, db.AuthenticateSessionParams{
		Now:              toPgTime(now),
		NewIdleExpiresAt: toPgTime(now.Add(IdleSessionTTL)),
		TokenHash:        hash[:],
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrUnauthenticated
	}
	if err != nil {
		return Actor{}, unavailable(err)
	}
	return Actor{
		UserID:          fromPgUUID(row.UserID),
		SessionID:       fromPgUUID(row.ID),
		DeviceID:        fromPgUUID(row.DeviceID),
		AccessState:     row.AccessState,
		MFARequired:     row.MfaRequired,
		SecurityVersion: row.SecurityVersion,
		tokenHash:       hash,
	}, nil
}

// Issue is a trusted in-process use case for verified login/MFA; it has no
// HTTP route in this slice. deviceToken is the raw cookie value, if any.
func (s *Sessions) Issue(ctx context.Context, userID uuid.UUID, deviceToken string, browser Browser, osLabel OS) (IssueResult, error) {
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return IssueResult{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	user, err := queries.LockIdentityUser(ctx, toPgUUID(userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return IssueResult{}, ErrUnauthenticated
	}
	if err != nil {
		return IssueResult{}, unavailable(err)
	}
	if user.AccessState != "active" {
		return IssueResult{}, ErrUnauthenticated
	}

	var device db.Device
	var rawDeviceToken string
	reused := false
	if hash, ok := decodeToken(deviceToken); ok {
		existing, err := queries.GetDeviceByBinding(ctx, db.GetDeviceByBindingParams{
			UserID: toPgUUID(userID), BindingHash: hash[:],
		})
		switch {
		case err == nil:
			device = existing
			reused = true
		case errors.Is(err, pgx.ErrNoRows):
			// fall through to create a fresh device association
		default:
			return IssueResult{}, unavailable(err)
		}
	}
	if !reused {
		count, err := queries.CountActiveDevices(ctx, toPgUUID(userID))
		if err != nil {
			return IssueResult{}, unavailable(err)
		}
		if count >= maxActiveDevices {
			return IssueResult{}, ErrDeviceLimit
		}
		deviceID, err := uuid.NewV7()
		if err != nil {
			return IssueResult{}, internalFailure(err)
		}
		raw, hash, err := newRawToken()
		if err != nil {
			return IssueResult{}, internalFailure(err)
		}
		rawDeviceToken = raw
		device, err = queries.InsertDevice(ctx, db.InsertDeviceParams{
			ID: toPgUUID(deviceID), UserID: toPgUUID(userID), BindingHash: hash[:],
			Browser: normalizeBrowser(browser), Os: normalizeOS(osLabel), Now: toPgTime(now),
		})
		if err != nil {
			return IssueResult{}, unavailable(err)
		}
	} else if err := queries.TouchDevice(ctx, db.TouchDeviceParams{ID: device.ID, Now: toPgTime(now)}); err != nil {
		return IssueResult{}, unavailable(err)
	}

	sessionID, err := uuid.NewV7()
	if err != nil {
		return IssueResult{}, internalFailure(err)
	}
	rawSessionToken, sessionHash, err := newRawToken()
	if err != nil {
		return IssueResult{}, internalFailure(err)
	}
	absoluteExpiresAt := now.Add(AbsoluteSessionTTL)
	session, err := queries.InsertSession(ctx, db.InsertSessionParams{
		ID: toPgUUID(sessionID), UserID: toPgUUID(userID), DeviceID: device.ID,
		TokenHash:         sessionHash[:],
		Now:               toPgTime(now),
		IdleExpiresAt:     toPgTime(now.Add(IdleSessionTTL)),
		AbsoluteExpiresAt: toPgTime(absoluteExpiresAt),
	})
	if err != nil {
		return IssueResult{}, unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return IssueResult{}, unavailable(err)
	}
	return IssueResult{
		SessionToken:      rawSessionToken,
		DeviceToken:       rawDeviceToken,
		SessionID:         fromPgUUID(session.ID),
		DeviceID:          fromPgUUID(device.ID),
		AbsoluteExpiresAt: absoluteExpiresAt,
	}, nil
}

// Rotate replaces a session's token hash in place, preserving its stable ID,
// created_at and absolute expiry. It has no HTTP route in this slice.
func (s *Sessions) Rotate(ctx context.Context, rawToken string) (RotateResult, error) {
	hash, ok := decodeToken(rawToken)
	if !ok {
		return RotateResult{}, ErrUnauthenticated
	}
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RotateResult{}, unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	existing, err := queries.FindSessionByTokenHash(ctx, hash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return RotateResult{}, ErrUnauthenticated
	}
	if err != nil {
		return RotateResult{}, unavailable(err)
	}

	user, err := queries.LockIdentityUser(ctx, existing.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RotateResult{}, ErrUnauthenticated
		}
		return RotateResult{}, unavailable(err)
	}
	if user.AccessState != "active" {
		return RotateResult{}, ErrUnauthenticated
	}

	locked, err := queries.LockActiveSession(ctx, db.LockActiveSessionParams{
		TokenHash: hash[:], UserID: existing.UserID, Now: toPgTime(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RotateResult{}, ErrUnauthenticated
	}
	if err != nil {
		return RotateResult{}, unavailable(err)
	}

	rawNew, newHash, err := newRawToken()
	if err != nil {
		return RotateResult{}, internalFailure(err)
	}
	rotated, err := queries.RotateSessionToken(ctx, db.RotateSessionTokenParams{
		TokenHash: newHash[:], Now: toPgTime(now), ID: locked.ID,
	})
	if err != nil {
		return RotateResult{}, unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return RotateResult{}, unavailable(err)
	}
	return RotateResult{
		SessionToken:      rawNew,
		SessionID:         fromPgUUID(rotated.ID),
		AbsoluteExpiresAt: rotated.AbsoluteExpiresAt.Time,
	}, nil
}

// lockAuthorizingSession locks the owning user row, then rechecks that the
// authorizing session/token is still active, under that lock. This prevents
// a revoke-after-revoke race from resurrecting a session.
func (s *Sessions) lockAuthorizingSession(ctx context.Context, queries *db.Queries, actor Actor, now time.Time) error {
	user, err := queries.LockIdentityUser(ctx, toPgUUID(actor.UserID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return unavailable(err)
	}
	if user.AccessState != "active" {
		return ErrUnauthenticated
	}
	_, err = queries.LockActiveSession(ctx, db.LockActiveSessionParams{
		TokenHash: actor.tokenHash[:], UserID: toPgUUID(actor.UserID), Now: toPgTime(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return unavailable(err)
	}
	return nil
}

// RevokeSession revokes one owned session. Already-revoked owned targets are
// idempotent; a nonowned or nonexistent target is reported as not found.
func (s *Sessions) RevokeSession(ctx context.Context, actor Actor, targetID uuid.UUID) error {
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	if err := s.lockAuthorizingSession(ctx, queries, actor, now); err != nil {
		return err
	}
	if _, err := queries.FindOwnedSession(ctx, db.FindOwnedSessionParams{
		ID: toPgUUID(targetID), UserID: toPgUUID(actor.UserID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return unavailable(err)
	}
	if err := queries.RevokeSession(ctx, db.RevokeSessionParams{ID: toPgUUID(targetID), Now: toPgTime(now)}); err != nil {
		return unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable(err)
	}
	return nil
}

// RevokeDeviceSessions revokes one owned device association and all of its
// sessions atomically.
func (s *Sessions) RevokeDeviceSessions(ctx context.Context, actor Actor, targetDeviceID uuid.UUID) error {
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	if err := s.lockAuthorizingSession(ctx, queries, actor, now); err != nil {
		return err
	}
	if _, err := queries.FindOwnedDevice(ctx, db.FindOwnedDeviceParams{
		ID: toPgUUID(targetDeviceID), UserID: toPgUUID(actor.UserID),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return unavailable(err)
	}
	if err := queries.RevokeDevice(ctx, db.RevokeDeviceParams{ID: toPgUUID(targetDeviceID), Now: toPgTime(now)}); err != nil {
		return unavailable(err)
	}
	if err := queries.RevokeDeviceSessions(ctx, db.RevokeDeviceSessionsParams{DeviceID: toPgUUID(targetDeviceID), Now: toPgTime(now)}); err != nil {
		return unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable(err)
	}
	return nil
}

// RevokeAllSessions revokes every session owned by the actor, including the
// current one.
func (s *Sessions) RevokeAllSessions(ctx context.Context, actor Actor) error {
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	if err := s.lockAuthorizingSession(ctx, queries, actor, now); err != nil {
		return err
	}
	if err := queries.RevokeAllSessions(ctx, db.RevokeAllSessionsParams{UserID: toPgUUID(actor.UserID), Now: toPgTime(now)}); err != nil {
		return unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable(err)
	}
	return nil
}

// Logout revokes the session identified by rawToken if one exists. It is
// idempotent: a malformed, unknown or already-revoked token is a no-op
// success, matching the transport-level logout contract.
func (s *Sessions) Logout(ctx context.Context, rawToken string) error {
	hash, ok := decodeToken(rawToken)
	if !ok {
		return nil
	}
	now := s.now().UTC()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return unavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := db.New(tx)

	initial, err := queries.FindSessionByTokenHash(ctx, hash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if _, err := queries.LockIdentityUser(ctx, initial.UserID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return unavailable(err)
	}
	// Re-read by token hash under the user lock: a rotation that committed
	// while this call was waiting for the lock replaces the hash, so the
	// old token becomes a no-op rather than revoking the new session.
	current, err := queries.FindSessionByTokenHash(ctx, hash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if err := queries.RevokeSession(ctx, db.RevokeSessionParams{ID: current.ID, Now: toPgTime(now)}); err != nil {
		return unavailable(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return unavailable(err)
	}
	return nil
}

// ListSessions returns up to one page of the owner's active, nonexpired
// sessions ordered by created_at DESC, id DESC, plus whether more remain.
func (s *Sessions) ListSessions(ctx context.Context, userID, currentSessionID uuid.UUID, after *SessionCursor) ([]SessionSummary, bool, error) {
	afterCreatedAt := farFutureTimestamp
	afterID := maxUUID
	if after != nil {
		afterCreatedAt = toPgTime(after.CreatedAt)
		afterID = toPgUUID(after.ID)
	}
	rows, err := db.New(s.pool).ListActiveSessions(ctx, db.ListActiveSessionsParams{
		UserID: toPgUUID(userID), Now: toPgTime(s.now().UTC()),
		AfterCreatedAt: afterCreatedAt, AfterID: afterID, RowLimit: sessionPageSize + 1,
	})
	if err != nil {
		return nil, false, unavailable(err)
	}
	hasMore := len(rows) > sessionPageSize
	if hasMore {
		rows = rows[:sessionPageSize]
	}
	items := make([]SessionSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, SessionSummary{
			ID: fromPgUUID(row.ID), DeviceID: fromPgUUID(row.DeviceID),
			CreatedAt: row.CreatedAt.Time, LastSeenAt: row.LastSeenAt.Time,
			IdleExpiresAt: row.IdleExpiresAt.Time, AbsoluteExpiresAt: row.AbsoluteExpiresAt.Time,
			Current: fromPgUUID(row.ID) == currentSessionID,
		})
	}
	return items, hasMore, nil
}

// ListDevices returns the owner's nonrevoked device associations ordered by
// created_at DESC, id DESC, bounded by the device cap.
func (s *Sessions) ListDevices(ctx context.Context, userID, currentDeviceID uuid.UUID) ([]DeviceSummary, error) {
	rows, err := db.New(s.pool).ListDevices(ctx, db.ListDevicesParams{
		UserID: toPgUUID(userID), RowLimit: maxActiveDevices,
	})
	if err != nil {
		return nil, unavailable(err)
	}
	items := make([]DeviceSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, DeviceSummary{
			ID: fromPgUUID(row.ID), Browser: row.Browser, OS: row.Os,
			CreatedAt: row.CreatedAt.Time, LastSeenAt: row.LastSeenAt.Time,
			Current: fromPgUUID(row.ID) == currentDeviceID,
		})
	}
	return items, nil
}
