# Increment 01: Session Foundation

Date: 2026-10-10. Status: implementation-ready brief after pre-task consistency review. Follow the [specification](../spec.md), [working plan](../plan.md) and [full contract](../contracts/identity.md). This is the first backend slice of increment 1, not all of Stage 1B. Generate/check executable tasks against this brief before editing application code.

## Outcome and Boundary

Deliver working server-side session issuance/rotation internally, session authentication, owner-only session/device inspection and revocation, and browser CSRF context. Exercise the complete slice with real PostgreSQL fixtures and HTTP/TLS test clients. There is no public registration/login or test-login endpoint. Application users cannot create an account/session until the next increment supplies real credential verification.

Included FR coverage: FR-009/010/011; the session ownership/current-state part of FR-013 and no-store/safe-error/telemetry part of FR-014. Other FRs remain assigned to later increments. No auth provider, OTP, password, MFA, recovery, delivery worker, profile or settings UI is implemented here. Preserve the current web status page and regenerate its API client only. Authenticated SSR, user-facing session controls and actual browser/HTTPS acceptance join the first usable sign-in journey; HTTP tests here must not be reported as real-browser acceptance.

## Existing Seams and Exact File Reach

| File | Required change |
| --- | --- |
| `api/openapi.yaml` | Add only the eight endpoints and schemas below; explicit cookie auth and safe errors |
| `server/migrations/00002_identity_sessions.sql` | Three tables: users, devices, sessions; up/down in dependency order |
| `server/queries/identity_sessions.sql` | Named generated queries listed below; no handwritten repository abstraction |
| `server/internal/db/*` | Regenerate sqlc; never edit manually |
| `server/internal/modules/identity/sessions.go` | Concrete Sessions service over pgxpool, token issuance/auth/rotation and owner operations |
| `server/internal/modules/identity/sessions_integration_test.go` | Real-DB fixtures, ownership, expiry, rotation/revocation races; fixtures only |
| `server/internal/transport/httpapi/identity.go` | Strict endpoint handlers and safe domain-error mapping |
| `server/internal/transport/httpapi/browser_security.go` | Cookie helpers, signed browser context, CSRF, exact Origin, signed list cursors; private transport helpers |
| `server/internal/transport/httpapi/identity_test.go`, `browser_security_test.go` | HTTP/TLS cases and real-DB API tests; no mock database |
| `server/internal/transport/httpapi/handler.go` | Wire service/config and generated parser/strict safe-error handlers |
| `server/internal/transport/httpapi/api.gen.go` | Regenerate OpenAPI output |
| `server/internal/platform/config/identity.go`, `identity_test.go` | API-only auth configuration; existing worker configuration unaffected |
| `server/internal/app/api.go`, `failure.go`, `process_test.go` | Compose Identity; safe invalid-auth-config code; preserve database startup canary |
| existing `httpapi/handler_test.go`, `observability/otel_test.go` | Update NewHandler callers for explicit new dependencies; preserve existing assertions |
| `server/go.mod` | Promote already present `github.com/google/uuid v1.6.0` from indirect if directly used; no version upgrade |
| `web/app/api/generated/*` | Regenerate only; no UI/composable/SSR behavior change |
| `.env.example`, `compose.yaml`, `README.md` | Explain/give key-generation setup and pass only API auth configuration; no shipped secret |

Set the constructor signature to `NewHandler(health system.HealthChecker, sessions *identity.Sessions, identityConfig config.Identity, middleware ...func(http.Handler) http.Handler)`. Tests confined to health may pass nil Sessions plus valid test config; nonhealth handlers must not dereference nil. RunAPI always supplies the real service. Existing `httpapi.NewHandler` wraps request-id outside observability and generated routing; preserve that order. `db.Queries.WithTx` and pgxpool are the transaction seams. `testdb.URL` fails without TEST_DATABASE_URL in CI and otherwise skips: acceptance requires a configured real test database, not a skipped result. Existing job tests truncate jobs; new tests must use unique users and targeted cleanup, never truncate shared tables across packages.

## Minimum Persistence

Create only fields used by this slice. Later increments add credential/strong-proof columns rather than requiring empty future tables now.

- `users`: id UUID PK; access_state constrained to pending_email/active/restricted/deletion_pending; mfa_required boolean default false; protection_state constrained to never_enrolled/protected/recovery_required, default never_enrolled; security_version bigint positive default 1; created_at/updated_at timestamptz. No profile fields. Test fixtures insert active users directly; no production create-user API/query is needed yet.
- `devices`: id UUID PK, user_id FK, binding_hash bytea (32 bytes), browser/OS allowlisted coarse labels, created_at/last_seen_at, revoked_at nullable; unique(user_id,binding_hash) and unique(id,user_id). No raw user agent or IP. Label input to the internal issuer is already reduced to Chrome/Edge/Firefox/Safari/Other and Windows/macOS/Linux/Android/iOS/Other; unknown is Other, not arbitrary caller text.
- `sessions`: id UUID PK, user_id FK, device_id, token_hash bytea (32 bytes) unique, created_at/last_seen_at, idle_expires_at, absolute_expires_at, revoked_at nullable. Composite FK(device_id,user_id) references devices(id,user_id), preventing cross-owner associations. Index active user rows by created_at/id and device_id. First-factor/strong-purpose metadata is introduced with real credential flows, not fabricated now.
- All IDs use the existing UUID library's UUIDv7 generator; timestamps UTC. Down migration drops sessions, devices, users and never modifies jobs.

Named query responsibilities (use generated DBTX-compatible methods): `LockIdentityUser`; `GetDeviceByBinding`; `CountActiveDevices`; `InsertDevice`; `TouchDevice`; `InsertSession`; `FindSessionByTokenHash`; `AuthenticateSession`; `LockActiveSession`; `RotateSessionToken`; `ListActiveSessions`; `ListDevices`; `FindOwnedSession`; `FindOwnedDevice`; `RevokeSession`; `RevokeDevice`; `RevokeDeviceSessions`; `RevokeAllSessions`.

`AuthenticateSession` is an atomic conditional UPDATE/RETURNING: join user/device, require active user, nonrevoked device/session and both deadlines > current time; update only last_seen_at and idle_expires_at=min(now+7 days, absolute_expires_at). Passive activity accounting is allowed on reads; it never creates credentials, sessions or privileges. All time-sensitive service operations take one UTC time from an injected `now func() time.Time` (default time.Now); pass it to queries so boundary tests do not sleep. Do not add a clock interface/framework.

## Internal Session Service

Use a concrete `identity.Sessions` built by `NewSessions(pool *pgxpool.Pool)` with an unexported now function defaulting to time.Now; same-package tests replace it. No extra production test-clock API is required. Expose only the necessary methods to the HTTP layer: Authenticate, ListSessions, ListDevices, RevokeSession, RevokeDeviceSessions, RevokeAllSessions and Logout. `Issue` and `Rotate` are trusted in-process use cases for later verified login/MFA; they have no HTTP route in this slice. Tests may call these directly.

- `Issue(ctx,userID,deviceToken,coarseLabels)` accepts an already verified user ID only from trusted code; lock the user, require active state, reuse a matching nonrevoked owned device or create one. Generate a fresh device token if absent/invalid/revoked. At 100 nonrevoked device associations, reject creation with `device_limit`; reuse still works. Then create one session. Return raw session/device tokens only to the transport caller, together with IDs and absolute expiry; never store raw tokens or return them in JSON.
- Session/device tokens are independent 32-byte cryptographically random values encoded as unpadded base64url. Store SHA-256 hashes. Malformed session tokens fail as unauthenticated without a database lookup. Use standard-library crypto/rand, sha256, hmac and constant-time comparison; no new dependency for these operations.
- Issue uses shared fixed constants in identity for a seven-day idle and thirty-day absolute lifetime; config.Identity does not duplicate these durations or expose runtime TTL options in this slice. Session cookie expiry follows the absolute deadline; server enforces the idle deadline. Device cookie lifetime is ninety days, matching inactive-device cleanup policy; actual bounded cleanup work comes before externally creatable account/device volume, not in this non-issuing HTTP slice.
- Rotate replaces the token hash in the same logical session record and returns a new cookie token; the server-side public list ID remains stable. Preserve original created_at and absolute expiry. Only the browser's authentication identifier rotates. Old token fails immediately; simultaneous rotations yield at most one success.
- Actor returned by authentication contains user/session/device IDs, account status, mfa_required and security_version; keep its current token hash private to server code. Do not expose the hash through API DTOs.
- Every modifying owner operation locks the user first, then rechecks the authorizing active session and current token before changing a target. Rotation first finds the owner, obtains that user lock, then rechecks the token under lock. This prevents rotate-after-revoke-all from resurrecting a session. Use one consistent user -> session/device lock order; commit/rollback on all paths.
- Revocation cannot be undone by last-seen updates. Requests beginning after revocation commits fail; already authorized in-flight work need not be retroactively cancelled. A newly verified future login after revocation remains allowed.
- Revoke-device marks the owned device association revoked and revokes all its sessions atomically. Revoke-all includes current session. Existing but already revoked owned targets remain idempotent; a nonowned/nonexistent target is indistinguishable 404. If the authorizing session has itself already been revoked, protected DELETEs return 401 on repetition; their state remains unchanged.

## Eight HTTP Endpoints

All defined endpoint responses, including auth errors, are no-store. Reads do not issue or rotate a session. Health endpoints retain existing behavior and do not require session/CSRF.

| Method/path | Result | Authentication and errors |
| --- | --- | --- |
| GET `/auth/context` | 200 `{csrf_token, enabled_methods: []}` for this slice; anonymous context cookie when needed | Anonymous allowed. With a current session derive session-bound CSRF; invalid/expired token becomes anonymous. No DB account/session creation. Database outage with a presented otherwise well-formed session: 503, not silently anonymous |
| GET `/identity/me` | 200 `{id,account_status,mfa_required,security_version}` | Active session; otherwise 401 |
| GET `/identity/sessions?cursor=...` | 200 `{items:[{id,device_id,created_at,last_seen_at,idle_expires_at,absolute_expires_at,current}],next_cursor?}` | Active owned nonexpired sessions only; page size 50; malformed/tampered/other-user cursor 400 |
| GET `/identity/devices` | 200 `{items:[{id,browser,os,created_at,last_seen_at,current}]}` | Nonrevoked owned associations; bounded by 100; stable order |
| POST `/auth/logout` | 204; clear session cookie | Valid Origin/CSRF; active or already expired/revoked/cleared context accepted as detailed below |
| DELETE `/identity/sessions/{id}` | 204; clear cookie if current | Active owner + Origin/CSRF; invalid UUID 400, missing/nonowned 404 |
| DELETE `/identity/devices/{id}/sessions` | 204; clear cookie if current device | Active owner + Origin/CSRF; invalid UUID 400, missing/nonowned 404 |
| DELETE `/identity/sessions` | 204; revoke all and clear cookie | Active owner + Origin/CSRF; UI confirmation later, no untyped request body |

Use `/api/v1` as existing base and operationIds `getAuthContext`, `getCurrentUser`, `listSessions`, `listDevices`, `logout`, `revokeSession`, `revokeDeviceSessions`, `revokeAllSessions`. Each new operation defines its success and ErrorResponse responses in OpenAPI. Represent no-body operations without ad hoc JSON payloads. Unknown query parameters on list/context/me are rejected; cursor maximum 1024 bytes. Reject nonempty bodies on these bodyless operations as invalid_request, enforce the global 16 KiB cap first.

Order both lists by created_at DESC, id DESC. Cursor is base64url JSON plus HMAC: version, user ID, last created_at and last UUID; authenticate it with domain-separated browser-key HMAC. Fetch 51 rows to return 50 plus next_cursor; bind cursor to owner and operation. No generic pagination framework. Devices fit the established 100-association bound and require no cursor now.

## Cookies, CSRF and Origin

Cookie names: `__Host-blob_session`, `__Host-blob_device`, `__Host-blob_context`. All use Secure, HttpOnly, Path=/, SameSite=Lax, no Domain. Only future successful verified session issuance sets session/device cookies; existing APIs clear session cookie using the same attributes, MaxAge=-1 and a past expiry. Never downgrade Secure for local HTTP tests.

Anonymous context is stateless: `v1.<expires Unix seconds>.<32-byte random base64url>.<HMAC base64url>`, expiry now+24h, signed with the browser key over a domain-separated canonical payload. Context GET reuses a valid cookie, replaces missing/invalid/expired cookie and returns only its derived CSRF token; no database write for anonymous context. The cookie is not authentication. Duplicate context cookies are invalid and replaced; duplicate session cookies are rejected as malformed authentication, not selected arbitrarily.

CSRF token is HMAC-SHA256 over a domain-separated message bound either to the full raw session token or to the validated anonymous context payload; return it as base64url from context GET. Verify `X-CSRF-Token` with constant-time comparison. A different browser/context, session or rotation invalidates the proof. Browser signing key is distinct from future OTP/encryption keys; domain-separate context, CSRF and cursor uses.

On mutations: first validate Origin byte-for-byte against configured canonical public origin; reject missing, null, lookalike/subdomain and untrusted origins with 403 origin_denied. Then validate authentication/CSRF and execute. Active-session mutation accepts only session-bound CSRF, never anonymous fallback. Logout is the one access-reducing exception: for an inactive well-formed session token, its matching session-derived CSRF can still clear it; alternatively a valid anonymous context obtained after expiry can clear the stale cookie. No-cookie logout accepts valid anonymous context CSRF and returns 204. Logout does not accept missing CSRF simply because no live session exists.

There is one configured trusted public origin; do not infer it from untrusted Host/X-Forwarded headers. Foreign-origin requests receive no CORS allow headers. For allowed preflight, return 204 with the exact origin, credentials, methods GET/POST/DELETE and header X-CSRF-Token only as needed; Vary: Origin. Same-origin browser calls need no cross-origin client option. CORS never substitutes for CSRF.

## Safe Error and Configuration Contract

Use the existing ErrorResponse envelope and requestid.FromContext. Exact safe mappings: 400 invalid_request; 401 authentication_required; 403 origin_denied/csrf_failed; 404 not_found; 409 device_limit; 413 request_too_large; 500 internal_error; 503 dependency_unavailable. Error text is fixed/safe and does not include SQL, token, cursor, request body or underlying err.Error(). Typed domain errors map centrally; pgx operational failures become dependency_unavailable, unexpected internal/random failures internal_error.

The generated router currently defaults to http.Error(err.Error()) for parameter failures. Explicitly replace `StdHTTPServerOptions.ErrorHandlerFunc` and strict request/response error callbacks, without editing generated code. Malformed UUID/cursor and service failures must follow the envelope. Native unknown-path/method responses are outside these eight contract paths; do not add a second router or global error framework.

Add API-only `config.LoadIdentity()` returning canonical `AUTH_PUBLIC_ORIGIN`, 32-byte standard-base64-decoded `AUTH_BROWSER_KEY` only. Fixed session durations live once in identity, not in configuration. Public origin must be HTTPS with no userinfo/path/query/fragment; a trailing slash is rejected rather than silently broadening comparisons. Missing/malformed origin or key fails API startup with `identity_config_invalid`; never log supplied values. The worker does not read or receive this key.

Compose passes `AUTH_PUBLIC_ORIGIN: ${AUTH_PUBLIC_ORIGIN:-https://localhost:8443}` and `AUTH_BROWSER_KEY: ${AUTH_BROWSER_KEY:-}` to API only. Do NOT use mandatory `${...:?}` interpolation: existing CI runs `docker compose config --quiet` without a .env. Missing key fails safely at API startup, not Compose parsing. `.env.example` documents blank key plus the generation procedure; do not commit a real or default signing secret.

README local-start setup must include this process-local generation command before Compose startup (preserve any existing .env rather than overwrite it):

```powershell
$blobKeyBytes = New-Object byte[] 32
$blobRng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $blobRng.GetBytes($blobKeyBytes) } finally { $blobRng.Dispose() }
$env:AUTH_BROWSER_KEY = [Convert]::ToBase64String($blobKeyBytes)
$env:AUTH_PUBLIC_ORIGIN = 'https://localhost:8443'
```

Persist the key through deployment secret configuration when sessions are user-facing. The existing Compose HTTP health/status origin stays usable; the HTTPS example identifies the upcoming trusted browser origin, not an already configured HTTPS listener. Actual browser session UI/HTTPS setup is deliberately next slice and must be stated plainly. TLS handler tests use a trusted httptest TLS client.

Preserve `TestProcessHidesMalformedDatabaseConfig`: supply a valid ephemeral test origin/key in the subprocess fixture, so adding auth requirements does not mask the expected database_config_invalid result for either binary. Add a separate invalid-auth-key subprocess/config canary. Startup may initialize Identity after database config validation; do not change existing database error meanings.

## Verification Required Before Completion

1. Real PostgreSQL, real up migration: fixture users A/B, Issue directly in trusted test code, HTTP me/list/revoke. Wrong-owner device/session IDs return 404; composite ownership FK rejects inconsistent rows. No login route appears in the OpenAPI contract.
2. Token hash-only storage, valid format, active-account requirement, idle/absolute boundary and revoked-device denial. Injectable clock proves seven/thirty-day behavior without sleeps. Cookie helpers assert every required attribute and clearing behavior.
3. Concurrent Rotate/Rotate and Rotate/RevokeAll, plus Authenticate racing revocation: one current token at most, no resurrection and no expiry extension beyond absolute deadline. Failed transactions leave no partial device/session creation.
4. Origin/CSRF matrix: missing/null/foreign origin, missing/wrong/other-session/stale token, valid anonymous context, context tamper/expiry, active logout, repeated logout and cleared-cookie logout. No GET changes account/authentication state; anonymous context creates no DB rows.
5. List pagination >50 with timestamp ties; no duplicates while walking a static fixture, correct current flag, cursor tamper/owner mismatch rejection and device cap behavior.
6. JSON errors for malformed UUID, query/body and database failure; no-store on all defined responses/errors. Secret canary covering cookies/CSRF/key/cursor passes existing allowlist logging/tracing path. Existing health/observability/process tests remain green.
7. Run Go tests with TEST_DATABASE_URL set, go vet/build, real up/down/up on a disposable DB, generation twice with no second-run drift, web typecheck/build against regenerated client, Compose config without .env and with test key. Record skipped browser/SSR acceptance accurately.

Generation drift script currently treats intended uncommitted contract/handler changes as dirty. Validate a before/after snapshot of generated paths, or run its normal check after the approved commit; do not misreport that expected working-tree dirt as a generator failure and do not alter the script for this feature.

## Pre-Task Analysis and Remaining Gates

Resolved blockers for this slice: generated error leakage, undefined CSRF/session transport details, logout retry semantics, rotation/revocation locking and API-only configuration versus worker/startup tests. This brief fixes those choices; implementation must not invent alternatives.

No unresolved product/provider/domain decision blocks this backend slice. Later browser proof still needs trusted HTTPS, and later live-delivery acceptance needs external configuration. No claim is made that those gates are complete.

The full Spec Kit analyze command requires tasks.md; this document records the earlier design review only. Run task coverage/consistency analysis after task generation. If a required operation cannot meet this brief, return the specific contradiction to planning instead of adding a dev-login shortcut or weakening security.