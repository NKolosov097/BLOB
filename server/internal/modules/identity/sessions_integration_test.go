package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"blob/server/internal/platform/database"
	"blob/server/internal/testdb"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := database.Open(t.Context(), testdb.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// testClock gives tests a controllable, injectable now() so idle/absolute
// boundary assertions never need to sleep.
func testClock(start time.Time) (func() time.Time, func(time.Time)) {
	current := start
	get := func() time.Time { return current }
	set := func(t time.Time) { current = t }
	return get, set
}

func newTestSessions(pool *pgxpool.Pool, now func() time.Time) *Sessions {
	s := NewSessions(pool)
	s.now = now
	return s
}

func createUser(t *testing.T, pool *pgxpool.Pool, accessState string) uuid.UUID {
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

func TestIssueAndAuthenticateRoundTrip(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	now, _ := testClock(time.Now().UTC())
	s := newTestSessions(pool, now)

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	if issued.SessionToken == "" || issued.DeviceToken == "" {
		t.Fatal("expected fresh session and device tokens")
	}
	if !issued.AbsoluteExpiresAt.Equal(now().Add(AbsoluteSessionTTL)) {
		t.Fatalf("absolute expiry = %v", issued.AbsoluteExpiresAt)
	}

	actor, err := s.Authenticate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if actor.UserID != userID || actor.SessionID != issued.SessionID || actor.DeviceID != issued.DeviceID {
		t.Fatalf("unexpected actor: %+v", actor)
	}
	if actor.AccessState != "active" || actor.SecurityVersion != 1 || actor.MFARequired {
		t.Fatalf("unexpected actor account fields: %+v", actor)
	}

	// Reusing the device cookie must not mint a new device token or row.
	reissued, err := s.Issue(t.Context(), userID, issued.DeviceToken, BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	if reissued.DeviceToken != "" {
		t.Fatal("reused device association returned a fresh device token")
	}
	if reissued.DeviceID != issued.DeviceID {
		t.Fatal("reused device association changed device identity")
	}

}

func TestAuthenticateFailsClosedWithoutDatabaseLookup(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	pool.Close() // any real lookup would now fail

	for _, token := range []string{"", "not-base64url!!!", "dG9vLXNob3J0"} {
		if _, err := s.Authenticate(t.Context(), token); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("token %q: got %v, want ErrUnauthenticated without a database call", token, err)
		}
	}
}

func TestAuthenticateRejectsInactiveAccountRevokedDeviceAndExpiry(t *testing.T) {
	pool := testPool(t)
	// Postgres timestamptz has microsecond precision; align the injected
	// clock so round-tripped database timestamps compare exactly equal.
	start := time.Now().UTC().Truncate(time.Microsecond)
	now, setNow := testClock(start)
	s := newTestSessions(pool, now)

	t.Run("pending account", func(t *testing.T) {
		userID := createUser(t, pool, "pending_email")
		if _, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("revoked device", func(t *testing.T) {
		userID := createUser(t, pool, "active")
		issued, err := s.Issue(t.Context(), userID, "", BrowserFirefox, OSLinux)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(t.Context(), `UPDATE devices SET revoked_at = now() WHERE id = $1`, toPgUUID(issued.DeviceID)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("idle expiry boundary", func(t *testing.T) {
		userID := createUser(t, pool, "active")
		issued, err := s.Issue(t.Context(), userID, "", BrowserSafari, OSiOS)
		if err != nil {
			t.Fatal(err)
		}
		defer setNow(start)

		setNow(start.Add(IdleSessionTTL))
		if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("at the idle deadline: got %v", err)
		}
		setNow(start.Add(IdleSessionTTL + time.Second))
		if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("past the idle deadline: got %v", err)
		}
	})

	t.Run("absolute expiry caps idle refresh", func(t *testing.T) {
		userID := createUser(t, pool, "active")
		issued, err := s.Issue(t.Context(), userID, "", BrowserEdge, OSOther)
		if err != nil {
			t.Fatal(err)
		}
		defer setNow(start)

		// Re-authenticate at intervals below the seven-day idle lifetime so
		// the session never actually goes idle on the way to the absolute
		// deadline.
		const step = 6 * 24 * time.Hour
		elapsed := time.Duration(0)
		for elapsed+step < AbsoluteSessionTTL-time.Minute {
			elapsed += step
			setNow(start.Add(elapsed))
			if _, err := s.Authenticate(t.Context(), issued.SessionToken); err != nil {
				t.Fatalf("at %v: %v", elapsed, err)
			}
		}

		// Just inside the absolute deadline: authentication succeeds, but
		// idle_expires_at must be capped at absolute_expires_at, not
		// extended further.
		setNow(start.Add(AbsoluteSessionTTL - time.Minute))
		actor, err := s.Authenticate(t.Context(), issued.SessionToken)
		if err != nil {
			t.Fatal(err)
		}
		var idleExpiresAt time.Time
		if err := pool.QueryRow(t.Context(), `SELECT idle_expires_at FROM sessions WHERE id=$1`, toPgUUID(actor.SessionID)).Scan(&idleExpiresAt); err != nil {
			t.Fatal(err)
		}
		if !idleExpiresAt.Equal(issued.AbsoluteExpiresAt) {
			t.Fatalf("idle_expires_at = %v, want capped at absolute %v", idleExpiresAt, issued.AbsoluteExpiresAt)
		}

		// Exactly at the absolute deadline, authentication must fail: the
		// query requires absolute_expires_at strictly greater than now.
		setNow(start.Add(AbsoluteSessionTTL))
		if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("at the absolute deadline: got %v", err)
		}

		// Past the absolute deadline, authentication must fail outright.
		setNow(start.Add(AbsoluteSessionTTL + time.Second))
		if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("past the absolute deadline: got %v", err)
		}
	})
}

func TestIssueEnforcesDeviceLimitButReuseStillWorks(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	s := newTestSessions(pool, time.Now)

	var lastDeviceToken string
	var lastDeviceID uuid.UUID
	for i := 0; i < maxActiveDevices; i++ {
		issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
		if err != nil {
			t.Fatalf("device %d: %v", i, err)
		}
		lastDeviceToken = issued.DeviceToken
		lastDeviceID = issued.DeviceID
	}

	if _, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows); !errors.Is(err, ErrDeviceLimit) {
		t.Fatalf("got %v, want ErrDeviceLimit at the cap", err)
	}

	reused, err := s.Issue(t.Context(), userID, lastDeviceToken, BrowserChrome, OSWindows)
	if err != nil {
		t.Fatalf("reuse at the cap must still work: %v", err)
	}
	if reused.DeviceID != lastDeviceID || reused.DeviceToken != "" {
		t.Fatal("reuse at the cap did not reuse the existing device")
	}
}

func TestRotatePreventsResurrectionAfterRevokeAll(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	s := newTestSessions(pool, time.Now)

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAllSessions(t.Context(), actor); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rotate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v, want rotate-after-revoke-all to fail closed", err)
	}
	if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v, want revoked session to stay revoked", err)
	}
}

func TestConcurrentRotateYieldsAtMostOneSuccess(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	s := newTestSessions(pool, time.Now)

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 5
	var wg sync.WaitGroup
	results := make([]error, attempts)
	tokens := make([]string, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rotated, err := s.Rotate(t.Context(), issued.SessionToken)
			results[i] = err
			tokens[i] = rotated.SessionToken
		}(i)
	}
	wg.Wait()

	successes := 0
	var winningToken string
	for i, err := range results {
		if err == nil {
			successes++
			winningToken = tokens[i]
		} else if !errors.Is(err, ErrUnauthenticated) {
			t.Fatalf("attempt %d: unexpected error %v", i, err)
		}
	}
	if successes != 1 {
		t.Fatalf("got %d successful rotations, want exactly 1", successes)
	}

	actor, err := s.Authenticate(t.Context(), winningToken)
	if err != nil {
		t.Fatal(err)
	}
	if actor.SessionID != issued.SessionID {
		t.Fatal("rotation changed the stable session ID")
	}

	var sessionCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE user_id=$1`, toPgUUID(userID)).Scan(&sessionCount); err != nil {
		t.Fatal(err)
	}
	if sessionCount != 1 {
		t.Fatalf("concurrent rotation created %d session rows, want 1", sessionCount)
	}
}

// TestConcurrentRotateAndRevokeAllRaceIsSafe authorizes revoke-all with a
// separate, stable session for the same user, independent of the one being
// concurrently rotated. Revoke-all must always succeed and must invalidate
// both the authorizer and whatever the rotation produced.
func TestConcurrentRotateAndRevokeAllRaceIsSafe(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	s := newTestSessions(pool, time.Now)

	toRotate, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	authorizerIssued, err := s.Issue(t.Context(), userID, "", BrowserFirefox, OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := s.Authenticate(t.Context(), authorizerIssued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var rotateErr, revokeErr error
	var rotatedToken string
	wg.Add(2)
	go func() {
		defer wg.Done()
		rotated, err := s.Rotate(t.Context(), toRotate.SessionToken)
		rotateErr = err
		rotatedToken = rotated.SessionToken
	}()
	go func() {
		defer wg.Done()
		revokeErr = s.RevokeAllSessions(t.Context(), authorizer)
	}()
	wg.Wait()

	if revokeErr != nil {
		t.Fatalf("revoke-all authorized by a separate stable session must always succeed: %v", revokeErr)
	}
	if rotateErr != nil && !errors.Is(rotateErr, ErrUnauthenticated) {
		t.Fatalf("unexpected rotate error: %v", rotateErr)
	}

	if _, err := s.Authenticate(t.Context(), toRotate.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("original token survived revoke-all")
	}
	if rotateErr == nil {
		if _, err := s.Authenticate(t.Context(), rotatedToken); !errors.Is(err, ErrUnauthenticated) {
			t.Fatal("rotated token survived revoke-all")
		}
	}
	if _, err := s.Authenticate(t.Context(), authorizerIssued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("authorizing session survived its own revoke-all")
	}

	var activeCount int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL`, toPgUUID(userID)).Scan(&activeCount); err != nil {
		t.Fatal(err)
	}
	if activeCount != 0 {
		t.Fatalf("got %d active sessions after revoke-all, want 0", activeCount)
	}
}

// TestRevokeAllRejectsStaleAuthorizingActor retains the stale-actor
// assertion: an Actor captured before its own session was rotated away must
// not be able to authorize further mutation.
func TestRevokeAllRejectsStaleAuthorizingActor(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	s := newTestSessions(pool, time.Now)

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Rotate(t.Context(), issued.SessionToken); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAllSessions(t.Context(), actor); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v, want unauthenticated for a stale actor", err)
	}
}

func TestOwnerOperationsEnforceOwnership(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userA := createUser(t, pool, "active")
	userB := createUser(t, pool, "active")

	issuedA, err := s.Issue(t.Context(), userA, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	actorA, err := s.Authenticate(t.Context(), issuedA.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	issuedB, err := s.Issue(t.Context(), userB, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.RevokeSession(t.Context(), actorA, issuedB.SessionID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want not found for a nonowned session", err)
	}
	if err := s.RevokeDeviceSessions(t.Context(), actorA, issuedB.DeviceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want not found for a nonowned device", err)
	}
	if err := s.RevokeSession(t.Context(), actorA, uuid.Must(uuid.NewV7())); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want not found for a nonexistent session", err)
	}
}

// TestOwnerOperationsAreIdempotentForOwnedTargets keeps an independent
// active authorizer and repeats revocation of a different owned session and
// device; both attempts must succeed and the target stays revoked.
func TestOwnerOperationsAreIdempotentForOwnedTargets(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	authorizerIssued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	authorizer, err := s.Authenticate(t.Context(), authorizerIssued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	targetSession, err := s.Issue(t.Context(), userID, "", BrowserFirefox, OSLinux)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := s.RevokeSession(t.Context(), authorizer, targetSession.SessionID); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if _, err := s.Authenticate(t.Context(), targetSession.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("target session remained usable after idempotent revocation")
	}

	targetDevice, err := s.Issue(t.Context(), userID, "", BrowserSafari, OSiOS)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := s.RevokeDeviceSessions(t.Context(), authorizer, targetDevice.DeviceID); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	if _, err := s.Authenticate(t.Context(), targetDevice.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("target device session remained usable after idempotent revocation")
	}

	// The authorizer itself must remain active throughout.
	if _, err := s.Authenticate(t.Context(), authorizerIssued.SessionToken); err != nil {
		t.Fatalf("independent authorizer was unexpectedly affected: %v", err)
	}
}

// TestProtectedMutationFailsOnceAuthorizingSessionIsRevoked covers the
// specific case where a request revokes its own authorizing session: a
// further protected mutation using that same actor must fail
// unauthenticated rather than silently resurrecting or repeating it.
func TestProtectedMutationFailsOnceAuthorizingSessionIsRevoked(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeSession(t.Context(), actor, issued.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeAllSessions(t.Context(), actor); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v, want unauthenticated once the authorizing session is revoked", err)
	}
}

// TestRotateRequiresActiveAccount covers R04: Rotate must recheck the
// locked user's access_state, not only the session/device state.
func TestRotateRequiresActiveAccount(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)

	for _, state := range []string{"restricted", "deletion_pending", "pending_email"} {
		t.Run(state, func(t *testing.T) {
			userID := createUser(t, pool, "active")
			issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(t.Context(), `UPDATE users SET access_state=$1 WHERE id=$2`, state, toPgUUID(userID)); err != nil {
				t.Fatal(err)
			}
			var hashBefore []byte
			var absBefore time.Time
			if err := pool.QueryRow(t.Context(), `SELECT token_hash, absolute_expires_at FROM sessions WHERE id=$1`, toPgUUID(issued.SessionID)).Scan(&hashBefore, &absBefore); err != nil {
				t.Fatal(err)
			}

			if _, err := s.Rotate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("got %v, want unauthenticated for account state %s", err, state)
			}

			var hashAfter []byte
			var absAfter time.Time
			if err := pool.QueryRow(t.Context(), `SELECT token_hash, absolute_expires_at FROM sessions WHERE id=$1`, toPgUUID(issued.SessionID)).Scan(&hashAfter, &absAfter); err != nil {
				t.Fatal(err)
			}
			if string(hashBefore) != string(hashAfter) || !absBefore.Equal(absAfter) {
				t.Fatal("rotate mutated the session despite a rejected account state")
			}
		})
	}
}

// TestRotatePreservesIdentityAndRejectsOldToken covers the rotation
// coverage requested alongside idempotence: the old token is denied
// immediately, and created_at/absolute_expires_at never change.
func TestRotatePreservesIdentityAndRejectsOldToken(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	var createdAt, absoluteExpiresAt time.Time
	if err := pool.QueryRow(t.Context(), `SELECT created_at, absolute_expires_at FROM sessions WHERE id=$1`, toPgUUID(issued.SessionID)).Scan(&createdAt, &absoluteExpiresAt); err != nil {
		t.Fatal(err)
	}

	rotated, err := s.Rotate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.SessionID != issued.SessionID {
		t.Fatal("rotation changed the stable session ID")
	}
	if rotated.SessionToken == issued.SessionToken {
		t.Fatal("rotation did not change the token")
	}
	if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("old token still authenticates after rotation")
	}
	actor, err := s.Authenticate(t.Context(), rotated.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if actor.SessionID != issued.SessionID {
		t.Fatal("new token resolved to a different session")
	}

	var gotCreatedAt, gotAbsoluteExpiresAt time.Time
	if err := pool.QueryRow(t.Context(), `SELECT created_at, absolute_expires_at FROM sessions WHERE id=$1`, toPgUUID(issued.SessionID)).Scan(&gotCreatedAt, &gotAbsoluteExpiresAt); err != nil {
		t.Fatal(err)
	}
	if !gotCreatedAt.Equal(createdAt) || !gotAbsoluteExpiresAt.Equal(absoluteExpiresAt) {
		t.Fatal("rotation changed created_at or absolute_expires_at")
	}
}

func TestLogoutIsIdempotentForUnknownAndAlreadyRevokedTokens(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	if err := s.Logout(t.Context(), ""); err != nil {
		t.Fatalf("empty token logout must be a no-op success: %v", err)
	}
	if err := s.Logout(t.Context(), "not-a-real-token"); err != nil {
		t.Fatalf("malformed token logout must be a no-op success: %v", err)
	}

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Logout(t.Context(), issued.SessionToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("got %v, want the session to be revoked", err)
	}
	if err := s.Logout(t.Context(), issued.SessionToken); err != nil {
		t.Fatalf("logging out an already-revoked token must still succeed: %v", err)
	}
}

func TestDeviceRevocationCascadesToItsSessionsAtomically(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeDeviceSessions(t.Context(), actor, issued.DeviceID); err != nil {
		t.Fatal(err)
	}
	var deviceRevoked, sessionRevoked bool
	if err := pool.QueryRow(t.Context(), `SELECT revoked_at IS NOT NULL FROM devices WHERE id=$1`, toPgUUID(issued.DeviceID)).Scan(&deviceRevoked); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT revoked_at IS NOT NULL FROM sessions WHERE id=$1`, toPgUUID(issued.SessionID)).Scan(&sessionRevoked); err != nil {
		t.Fatal(err)
	}
	if !deviceRevoked || !sessionRevoked {
		t.Fatalf("device revocation did not cascade: device=%t session=%t", deviceRevoked, sessionRevoked)
	}
}

func TestListSessionsPaginatesWithoutDuplicatesOrGaps(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")
	s := newTestSessions(pool, time.Now)

	const total = 55
	ids := make(map[uuid.UUID]bool, total)
	var currentSessionID uuid.UUID
	for i := 0; i < total; i++ {
		issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
		if err != nil {
			t.Fatal(err)
		}
		ids[issued.SessionID] = true
		currentSessionID = issued.SessionID
	}
	// Tie every fixture row's created_at, so ordering is decided purely by
	// id DESC and the 50/51 page boundary necessarily falls on a tie,
	// rather than depending on which row a random pick happened to copy.
	if _, err := pool.Exec(t.Context(), `UPDATE sessions SET created_at = now() WHERE user_id=$1`, toPgUUID(userID)); err != nil {
		t.Fatal(err)
	}

	seen := map[uuid.UUID]bool{}
	var cursor *SessionCursor
	pages := 0
	for {
		items, hasMore, err := s.ListSessions(t.Context(), userID, currentSessionID, cursor)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		if pages > total {
			t.Fatal("pagination did not terminate")
		}
		for _, item := range items {
			if seen[item.ID] {
				t.Fatalf("duplicate session %s across pages", item.ID)
			}
			seen[item.ID] = true
			if item.Current != (item.ID == currentSessionID) {
				t.Fatalf("session %s current flag = %t", item.ID, item.Current)
			}
		}
		if !hasMore {
			break
		}
		last := items[len(items)-1]
		cursor = &SessionCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(seen) != total {
		t.Fatalf("saw %d sessions across pages, want %d", len(seen), total)
	}
	wantPages := total / sessionPageSize
	if total%sessionPageSize != 0 {
		wantPages++
	}
	if pages != wantPages {
		t.Fatalf("got %d pages, want %d", pages, wantPages)
	}
}

// TestListSessionsCursorIsScopedToOwner asserts the query-level ownership
// contract only: a cursor's (created_at, id) position is scoped to the
// user_id passed alongside it. Signed/tampered cursor rejection is a
// transport-layer concern, covered separately by the HTTP tests.
func TestListSessionsCursorIsScopedToOwner(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userA := createUser(t, pool, "active")
	userB := createUser(t, pool, "active")

	issuedA, err := s.Issue(t.Context(), userA, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	cursor := &SessionCursor{CreatedAt: issuedA.AbsoluteExpiresAt, ID: issuedA.SessionID}
	items, hasMore, err := s.ListSessions(t.Context(), userB, uuid.Nil, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || hasMore {
		t.Fatal("cursor positioned for one user leaked another user's sessions")
	}
}

func TestListDevicesReturnsOnlyNonrevokedOwnedAssociations(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	kept, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := s.Issue(t.Context(), userID, "", BrowserFirefox, OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), kept.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeDeviceSessions(t.Context(), actor, removed.DeviceID); err != nil {
		t.Fatal(err)
	}

	devices, err := s.ListDevices(t.Context(), userID, kept.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].ID != kept.DeviceID || !devices[0].Current {
		t.Fatalf("unexpected device list: %+v", devices)
	}
}

func TestTokensAreHashedNotStoredRaw(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	var sessionHash, deviceHash []byte
	if err := pool.QueryRow(t.Context(), `SELECT token_hash FROM sessions WHERE id=$1`, toPgUUID(issued.SessionID)).Scan(&sessionHash); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT binding_hash FROM devices WHERE id=$1`, toPgUUID(issued.DeviceID)).Scan(&deviceHash); err != nil {
		t.Fatal(err)
	}
	if len(sessionHash) != sha256.Size || len(deviceHash) != sha256.Size {
		t.Fatal("stored hashes are not 32 bytes")
	}
	computedHash, ok := decodeToken(issued.SessionToken)
	if !ok {
		t.Fatal("issued session token did not decode")
	}
	if string(computedHash[:]) != string(sessionHash) {
		t.Fatal("stored hash did not match sha256 of the issued token")
	}
}

// TestLogoutByStaleTokenAfterRotationDoesNotRevokeNewSession covers R03:
// logout must re-read the session by token hash under the owner lock, not
// revoke by a stable session ID captured before rotation.
func TestLogoutByStaleTokenAfterRotationDoesNotRevokeNewSession(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(t.Context(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, toPgUUID(userID)); err != nil {
		t.Fatal(err)
	}
	var blocker int
	if err := tx.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Logout(ctx, issued.SessionToken) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, blocker).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("logout completed before waiting for user lock: %v", err)
		case <-ctx.Done():
			t.Fatal("logout did not block on user lock")
		case <-ticker.C:
		}
	}
	replacement, hash, err := newRawToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET token_hash=$1 WHERE id=$2`, hash[:], toPgUUID(issued.SessionID)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("logout did not complete after lock release")
	}
	if _, err := s.Authenticate(t.Context(), replacement); err != nil {
		t.Fatalf("replacement session was revoked: %v", err)
	}
}

// TestAuthenticateRacesRevocationWithoutResurrection covers T04: concurrent
// Authenticate attempts racing a revocation must leave the session
// permanently revoked, never resurrected.
func TestAuthenticateRacesRevocationWithoutResurrection(t *testing.T) {
	pool := testPool(t)
	s := newTestSessions(pool, time.Now)
	userID := createUser(t, pool, "active")

	issued, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := s.Authenticate(t.Context(), issued.SessionToken)
	if err != nil {
		t.Fatal(err)
	}

	const concurrentReads = 20
	var wg sync.WaitGroup
	wg.Add(concurrentReads + 1)
	go func() {
		defer wg.Done()
		if err := s.RevokeSession(t.Context(), actor, issued.SessionID); err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < concurrentReads; i++ {
		go func() {
			defer wg.Done()
			_, _ = s.Authenticate(t.Context(), issued.SessionToken)
		}()
	}
	wg.Wait()

	if _, err := s.Authenticate(t.Context(), issued.SessionToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("session usable after revocation completed")
	}
}

// TestCrossOwnerSessionDeviceFKIsRejected covers T04: the composite
// FOREIGN KEY (device_id, user_id) REFERENCES devices(id, user_id) must
// reject a session row whose owner does not match its device's owner.
func TestCrossOwnerSessionDeviceFKIsRejected(t *testing.T) {
	pool := testPool(t)
	userA := createUser(t, pool, "active")
	userB := createUser(t, pool, "active")

	deviceID := uuid.Must(uuid.NewV7())
	bindingHash := sha256.Sum256([]byte("cross-owner-device-fixture"))
	if _, err := pool.Exec(t.Context(), `INSERT INTO devices (id,user_id,binding_hash,browser,os) VALUES ($1,$2,$3,'Chrome','Windows')`,
		toPgUUID(deviceID), toPgUUID(userA), bindingHash[:]); err != nil {
		t.Fatal(err)
	}

	sessionID := uuid.Must(uuid.NewV7())
	tokenHash := sha256.Sum256([]byte("cross-owner-session-fixture"))
	_, err := pool.Exec(t.Context(), `INSERT INTO sessions (id,user_id,device_id,token_hash,idle_expires_at,absolute_expires_at)
		VALUES ($1,$2,$3,$4, now() + interval '1 day', now() + interval '30 days')`,
		toPgUUID(sessionID), toPgUUID(userB), toPgUUID(deviceID), tokenHash[:])
	if err == nil {
		t.Fatal("expected the composite ownership FK to reject a cross-owner session/device pair")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("got %v, want a foreign key violation", err)
	}
}

// TestIssueTransactionRollsBackWithoutOrphanDevice covers T04: a forced
// failure on the session insert inside Issue's transaction must leave no
// orphan device row behind.
func TestIssueTransactionRollsBackWithoutOrphanDevice(t *testing.T) {
	pool := testPool(t)
	userID := createUser(t, pool, "active")

	constraint := pgx.Identifier{"issue_rollback_" + uuid.NewString()}.Sanitize()
	if _, err := pool.Exec(t.Context(), `ALTER TABLE sessions ADD CONSTRAINT `+constraint+` CHECK (user_id <> '`+userID.String()+`'::uuid)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `ALTER TABLE sessions DROP CONSTRAINT `+constraint); err != nil {
			t.Error(err)
		}
	})
	s := newTestSessions(pool, time.Now)
	_, err := s.Issue(t.Context(), userID, "", BrowserChrome, OSWindows)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable after forced session-insert failure", err)
	}
	for _, table := range []string{"sessions", "devices"} {
		var count int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM `+table+` WHERE user_id=$1`, toPgUUID(userID)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("Issue rollback left %d %s rows", count, table)
		}
	}
}
