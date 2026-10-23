# Session Foundation: Implementation Checkpoint Review

Date: 2026-10-10. Status: review corrections complete; final aggregate verification passed. Scope: the current first-slice implementation against [01-sessions.md](01-sessions.md). This is the historical correction record; current acceptance and verification are in [01-convergence.md](01-convergence.md). Paths below are relative to the repository root.

Final source-review status: R01–R12 and T01–T09 are corrected. R05 now rejects empty hostname/port and noncanonical or out-of-range ports; R11/R12 have length and HEAD guards. The rollback test calls real Issue, the logout race uses an observed blocking condition, and HTTP continuation/revocation/lifetime/telemetry cases are implemented. Earlier findings and checkpoint evidence below are retained for traceability; statements describing open items apply to their historical checkpoint. No remaining source-review blocker was identified. Final uncached full Go tests against real PostgreSQL, vet, build and diff checks passed with exit 0 and no test skips; evidence is recorded in the convergence record.

## Blocking Correctness Findings

### R01 — Session issuance cannot insert its timestamps

Status: source corrected. Issue now supplies its captured UTC time to `InsertSessionParams.Now`.

`server/internal/modules/identity/sessions.go`, `Issue`: `InsertSessionParams` omits `Now`. The query explicitly inserts that parameter into both non-null timestamps; the zero `pgtype.Timestamptz` is SQL NULL, not the database default.

Correction: pass the operation's existing injected UTC `now` into `InsertSessionParams.Now`.

Acceptance: real-DB Issue succeeds; session created/last-seen timestamps equal the injected time, with seven-day idle and thirty-day absolute deadlines. A failed issue leaves no partial device/session. Subsequent independent execution of `go test ./internal/modules/identity -count=1` with `CI=true` against the migrated disposable `blob_migration_roundtrip` database confirmed 14 failing cases at Issue: `created_at` not-null violation, SQLSTATE 23502. This is evidence for R01, not evidence that the later assertions are incorrect.

### R02 — Active logout accepts anonymous CSRF and leaves a usable session

Status: source corrected. Logout first checks current session state, rejects anonymous fallback for active sessions, and propagates dependency errors without clearing cookies.

`server/internal/transport/httpapi/identity.go`, `Logout`: anonymous-context fallback runs without determining whether the supplied session is active. An active browser can submit its earlier anonymous CSRF, receive 204 and a cleared cookie, while its server-side session remains usable.

Correction: distinguish active session from inactive/missing/malformed session before anonymous fallback. An active session accepts only its session-bound CSRF and must be revoked before clearing. For an inactive session retain the specified matching old session-CSRF or valid anonymous-context behavior. A well-formed cookie whose database lookup fails returns safe 503, without silently clearing it. Duplicate cookies must never select one value arbitrarily.

Acceptance: active cookie + valid anonymous CSRF returns 403 with no cookie clearing and no revocation; active cookie + session CSRF returns 204 and subsequently fails authentication. Expired/revoked/no-cookie fallback still works; a database outage with a well-formed cookie and anonymous proof returns 503 without Set-Cookie. Repeated and duplicate-cookie cases remain covered by the brief's matrix.

### R03 — Logout does not recheck its token after the owner lock

Status: source corrected. Logout now re-reads by token hash after the owner lock and returns an idempotent no-op when rotation removed that hash.

`server/internal/modules/identity/sessions.go`, `Logout`: the session is found before locking the owner, then revoked by stable session ID. A rotation that commits while logout waits replaces the token, but the old-token request still revokes the replacement.

Correction: after obtaining the owner lock, re-read the session by the supplied token hash before revocation. A rotated-away token is an idempotent no-op. Preserve user-before-session lock order and commit/rollback handling; do not require the session to be unexpired merely to log it out.

Acceptance: controlled Rotate/Logout ordering where rotation commits before logout's locked recheck leaves the new token valid. If logout obtains the lock first, rotation cannot resurrect the revoked session.

### R04 — Rotation ignores current account access state

Status: source corrected. Rotate checks the locked account is active before locking/updating the session.

`server/internal/modules/identity/sessions.go`, `Rotate`: the result of `LockIdentityUser` is discarded. `LockActiveSession` checks session/device state, but not account state.

Correction: require the locked user's `access_state` to be active before rotating, consistently with Issue, Authenticate and modifying owner operations.

Acceptance: issue while active, change the user to each nonactive state, then Rotate returns unauthenticated and does not alter the token hash or deadlines.

## Required Contract and Robustness Corrections

### R05 — Origin validation accepts values browsers cannot send as their origin

Status: source corrected, including the follow-up empty-host/empty-port/leading-zero/out-of-range cases in the config test table. Request-origin comparison remains exact.

`server/internal/platform/config/identity.go`, `validatePublicOrigin`: empty query/fragment delimiters pass (`https://example.com?`, `https://example.com#`), as do noncanonical host/default-port spellings. The stored string is later compared byte-for-byte with browser Origin, so these configurations cause every legitimate mutation to fail.

Correction: reject noncanonical origin input at startup, including empty query/fragment delimiters, uppercase DNS host and an explicit default HTTPS port. Retain canonical nondefault ports and HTTPS-only/no-userinfo/no-path rules; do not broaden request-origin matching.

Acceptance: those invalid examples fail with only `identity_config_invalid`; canonical `https://example.com` and `https://localhost:8443` remain accepted. Existing config tests cover ordinary query/fragment values, but not these cases.

### R06 — Request guards execute too late

Status: original ordering corrected in source. The outer identity guard now precedes generated parameter binding, checks Origin before body validation, and preserves no-store. Its new classifier requires the R11/R12 corrections below.

`server/internal/transport/httpapi/handler.go`, `identityCrossCutting`: generated UUID/query binding happens before strict middleware. A malformed target UUID bypasses both the Origin guard and body-size check. Within the middleware body validation also precedes Origin.

Correction: apply defined-identity-route no-store and mutation Origin checks before generated parameter binding, followed by the 16 KiB/bodyless checks before binding. On allowed-origin requests enforce the cap before classifying a smaller nonempty body. Keep health behavior, request-ID/observability order, safe parser callbacks and the existing router; no second router/framework is needed.

Acceptance: foreign/missing Origin + malformed UUID returns 403 `origin_denied`; allowed Origin + malformed UUID + body over 16 KiB returns 413; allowed Origin + malformed UUID + empty body returns 400. All use the safe JSON envelope and no-store. GET body-cap and ordinary nonempty-body cases also pass.

### R07 — Optional service wiring can panic

Status: source corrected. Service-dependent paths guard nil service and return dependency-unavailable; anonymous context remains stateless.

`server/internal/transport/httpapi/identity.go`: session operations dereference `h.sessions` without a nil guard, although the agreed constructor permits nil for health-only tests.

Correction: return safe dependency-unavailable when a request actually requires the absent session service. Anonymous context without a session can remain stateless. Keep existing health-only callers valid.

Acceptance: handler constructed with nil service retains health behavior; identity reads with a well-formed cookie and authorized mutation attempts return safe 503, not panic. Anonymous context with no cookie still works.

### R08 — Malformed query encoding is silently dropped

Status: source corrected. The outer guard uses `url.ParseQuery` and rejects parse errors before generated binding for the defined GET query allowlists.

`server/internal/transport/httpapi/handler.go`: `r.URL.Query()` discards malformed query entries before the unknown-key check, so `/auth/context?unknown=%zz` is treated as having no query.

Correction: parse the raw query with error handling before generated binding/allowlist validation and reject malformed input with 400. Retain the existing endpoint-specific allowlists and cursor bound.

Acceptance: malformed percent encoding and forbidden query keys return safe 400/no-store; an ordinary signed cursor still paginates successfully.

### R09 — OpenAPI omits implemented errors and required CSRF input

Status: source corrected. OpenAPI includes the missing responses and required reusable CSRF header; regenerated parameter errors use the typed missing-header mapping to 403, after the outer Origin guard. HTTP assertions remain pending.

`api/openapi.yaml`: the other database-backed identity operations omit 503; new GET operations omit 413; mutation request contracts do not declare the required `X-CSRF-Token` header or explain exact Origin validation.

Correction: complete the eight operations' response contracts, add a reusable required CSRF-header parameter to mutations, document Origin, then regenerate both clients. Integrate the required-header parser with the existing semantics: a missing CSRF header must return 403 `csrf_failed`, not generic parser 400; Origin rejection still takes precedence. A typed generated missing-header error mapping or a pre-parser presence check is sufficient.

Acceptance: contract validation covers those statuses and parameters; allowed Origin + missing CSRF returns 403/no-store even after regeneration, foreign Origin + missing CSRF returns `origin_denied`. Generated artifacts have no second-run drift.

### R10 — Random failures are misclassified and actor hash is exported

Status: source corrected. UUID/random failures use the internal-error path, database failures retain `ErrUnavailable`, and the actor's hash field is now unexported.

`server/internal/modules/identity/sessions.go`: UUID/random-generation errors in Issue/Rotate are wrapped as dependency-unavailable, contrary to the specified 500 internal-error mapping. `Actor.TokenHash` is exported despite the requirement that this recheck value stay private to the service.

Correction: reserve `ErrUnavailable` for database/dependency errors; let internal generation failures reach the safe 500 mapping. Make the actor hash field unexported and update internal references. Do not add a public random-source configuration or fault-injection framework.

Acceptance: domain-error mapping tests distinguish internal 500 from dependency 503 without leaking underlying details; service callers and generated DTOs do not expose the hash. Existing session behavior remains covered by integration tests.

### R11 — Overlapping route prefix/suffix causes a slice panic

Status: source corrected with a length check before slicing and HTTP malformed-path coverage.

`server/internal/transport/httpapi/handler.go`, `singlePathSegment`: it checks prefix and suffix independently, then slices `rest[len(prefix):len(rest)-len(suffix)]`. DELETE `/api/v1/identity/devices/sessions` matches both overlapping strings; the resulting lower bound exceeds the upper bound and panics before routing.

Correction: require enough length for the full prefix, a nonempty middle segment, and suffix before slicing. Leave nonexistent paths to the existing router.

Acceptance: this path, an empty target and valid/malformed target paths never panic; the nonexistent path retains ordinary routing behavior, while recognized operations retain Origin/body/parameter validation.

### R12 — HEAD bypasses the guard but reaches GET handlers

Status: source corrected by classifying HEAD with GET; all four read routes have HTTP HEAD status/no-store/empty-body coverage.

`server/internal/transport/httpapi/handler.go`, `classifyIdentityRequest`: it recognizes only GET for reads. The generated Go ServeMux GET routes also accept HEAD, so HEAD `/api/v1/auth/context` reaches `GetAuthContext` without the request-context value, then dereferences a nil request in `readCookie`.

Correction: classify HEAD consistently with GET before selecting read operations, so the same guard/context setup runs. Keep the existing router and health semantics.

Acceptance: HEAD against all four identity read routes has normal guarded status/no-store behavior without a panic and produces no response body under the HTTP server; health HEAD behavior remains unchanged.

## Reviewed Without Additional Findings

The migration's ownership/hash constraints, query pagination order, user-first modifying-operation locks, and browser helper cookie attributes/domain-separated HMAC/cursor ownership checks match the brief in the inspected snapshot. This is source review, not proof of their runtime behavior. Migration roundtrip and the complete test/generation gates belong in the separate verification record.

No existing dependency version was upgraded. UUID v1.6.0 is promoted to direct use. The generated parameter binding introduces `github.com/oapi-codegen/runtime` v1.7.0 and its jsonmerge dependency; retain this justified generated-code requirement without unrelated upgrades.

At the initial checkpoint only the new config tests were present. The session integration test file subsequently appeared and was reviewed below; browser and HTTP tests were still being written. R01 has the independently executed failure evidence recorded above; the other findings remain source-review observations. Recheck each correction against the completed tests before accepting the slice.

## Session Test Review Follow-up

Initial test-review snapshot: `server/internal/modules/identity/sessions_integration_test.go` had just appeared and the executed suite stopped its affected cases at R01. The test-design findings below concern assertions reached after that fix. Current correction-review status follows.

Correction review: T01, T02, T03 and T05 are corrected. T04 now contains meaningful FK rejection and concurrent authentication/revocation checks, but its rollback test does not call Issue; replace that test as described below. The query-ownership cursor test has also been renamed accurately. The latest real-DB suite passed; these remaining observations concern coverage effectiveness, not a failed run.

- **T01 — Incorrect absolute-expiry fixture.** `TestAuthenticateRejectsInactiveAccountRevokedDeviceAndExpiry` advances directly from issuance to thirty days minus one minute, after its seven-day idle lifetime has already expired. Refresh at intervals below seven days before asserting the absolute cap. Use a microsecond-aligned injected start time when comparing exact database timestamps, and assert rejection exactly at both deadlines as well as beyond them. This fixes the test, not the session expiry policy.
- **T02 — Incorrect race expectation.** `TestConcurrentRotateAndRevokeAllRaceIsSafe` authorizes revoke-all using the very token being rotated, then insists revocation always succeeds. If rotation wins, the required locked token recheck must reject that stale actor. Authorize revoke-all with a separate stable session for the same user; then it must succeed and invalidate both authorizer and any rotation result. Retain a separate stale-actor assertion rather than weakening production authorization.
- **T03 — Pagination tie case is not guaranteed.** The pagination test chooses a random map entry to copy the earliest timestamp onto; it can select that same earliest row, and any resulting tie is near the final rows rather than the page boundary. Give all 55 fixture sessions the same injected timestamp (or explicitly tie rows across the 50/51 boundary), then verify every expected ID appears once in two pages.
- **T04 — Required persistence/race cases are absent.** Add bounded real-DB checks for Authenticate racing revocation and subsequent denial/no resurrection, a cross-owner session/device composite-FK rejection, and rollback after a forced session-insert failure with no orphan device. Use fixture-local transactions/constraints and targeted cleanup, not production fault-injection APIs or shared-table truncation. These are required by the existing brief, not additional feature scope.
- **T05 — Idempotent owned-target behavior is not actually exercised.** The owner-operations test revokes its own authorizing session and then checks 401, which is useful but does not test successful repetition. Keep an independent active authorizer and revoke another owned session/device twice; both attempts must succeed and the target remains revoked. Rotation coverage should also assert old-token denial and unchanged creation/absolute-expiry timestamps.

`TestListSessionsRejectsForeignAndTamperedCursor` checks query ownership only; it does not process a signed or tampered cursor. Rename it to describe that assertion and retain actual signature/owner-mismatch rejection in the planned transport tests. Do not count this test toward cryptographic cursor acceptance.

Remaining focused test corrections:

- `TestIssueTransactionRollsBackWithoutOrphanDevice` creates, fails and rolls back a manual SQL transaction; it never invokes `Sessions.Issue`. It would pass even if Issue committed a device separately. Create a fresh fixture user, install a uniquely named CHECK constraint on sessions rejecting only that user's UUID (`user_id <> '<fixture UUID>'::uuid`), and immediately register cleanup to drop it. Use a safely quoted generated constraint identifier. Call real Issue with no device token, require the constraint-induced error, then query both tables by that user and require zero rows. Drop the constraint before normal fixture cleanup. This exercises Issue after its successful device insert without affecting other fixture users or adding a production injection interface.
- `TestLogoutByStaleTokenAfterRotationDoesNotRevokeNewSession` runs rotation fully before logout. It also passes the original broken implementation because the first token lookup already finds nothing. Use this minimal deterministic overlap test: issue a real session; begin a fixture transaction and lock its user row; obtain that transaction's `pg_backend_pid()`; start real Logout with the old token in a goroutine and a bounded cancellable context. Poll `pg_stat_activity`/`pg_blocking_pids(pid)` until a backend is observed blocked by that fixture PID (the user's unique row identifies this test's Logout wait); a timeout is a test failure, not success. While still holding the user lock, generate a new token/hash with the existing same-package token helper and update the session hash in the held transaction, preserving ID/deadlines to simulate committed rotation. Commit, await successful Logout through a buffered result channel, then authenticate the replacement token and require success. Register rollback/cancellation before starting the goroutine, and bound its completion wait. This deterministically places Logout's first lookup before the token replacement and its recheck after it, without production hooks or arbitrary sleeps as ordering proof.

## HTTP Acceptance Review Follow-up

The completed HTTP/browser test package passed independently against real PostgreSQL (`CI=true`, `go test ./internal/transport/httpapi -count=1`, 3.943s). It uses `httptest.NewTLSServer` and the server's trusted client, satisfying this slice's TLS-client requirement. Cookie attributes/clearing, helper signature checks, and cursor owner/operation/tamper checks are covered. This remains HTTP-client testing, not deferred real-browser/SSR acceptance. No additional production issue was found beyond R05/R11/R12.

The following four bounded test corrections complete existing brief acceptance; reuse the present fixtures and helpers rather than adding test frameworks:

- **T06 — Follow the actual HTTP continuation cursor.** `TestListSessionsPaginatesWithSignedCursor` currently creates three sessions and asserts no cursor exists. Issue 55 sessions for one fixture user, passing the first returned device token to subsequent Issue calls to reuse one device. Authenticate requests with one live session cookie. Fetch page one, require 50 items and a nonempty next_cursor, then send that exact returned cursor in page two and require the remaining five items and no continuation. Verify expected IDs appear exactly once and only the authenticating session is marked current. Existing separate tests retain tamper/foreign-owner rejection.
- **T07 — Exercise the two remaining revocation routes through HTTP.** With fresh fixtures, call DELETE `/identity/devices/{id}/sessions` against a different user's device and require 404 without changing its session. For the current owned device, create two sessions reusing that device token, revoke it, require 204 plus correctly expired session cookie, and verify both tokens fail authentication. In a separate fixture with sessions on two devices, call DELETE `/identity/sessions`, require 204 plus cleared cookie, and verify both tokens fail authentication. These tests complement service tests by checking routing, authorization, target binding and response cookie handling.
- **T08 — Complete the handler-level cookie/CSRF boundary matrix.** Extend existing TLS tests using fixture SQL to expire a session deadline in the past, without sleeps: expired cookie with its old session-bound CSRF clears successfully; expired cookie with a valid anonymous context/CSRF also clears. Rotate a session internally, then send its new cookie with the previous CSRF and require 403 with no cookie clearing. Send duplicate session cookies to an authenticated read and require 401 rather than choosing one. Send two context cookies to context GET and require a fresh valid replacement; likewise replace a context signed with a time older than its 24-hour lifetime. An expired context must not authorize no-session logout. Use existing signing helpers only to create fixtures, and check the HTTP responses/remaining session state. Retain existing active-session anonymous-fallback rejection and outage coverage.
- **T09 — Exercise identity secrets and safe responses through telemetry.** Extend the existing `server/internal/platform/observability/otel_test.go` allowlist/receiver setup with defined identity requests carrying unique session/context cookie, `X-CSRF-Token` and cursor canaries, and use a known configured browser key whose standard-base64 representation is also checked. Include a malformed UUID parser rejection and a safe dependency-unavailable response (the supported nil session service suffices for that wiring check). Assert none of the canaries appears in captured logs, exported traces or error bodies; the existing receiver/export flush path is sufficient. Check safe error code/request ID and `Cache-Control: no-store` for these errors, and add no-store assertions to the existing successful context/read/revocation cases. Do not alter telemetry's allowlist or add a separate logging framework. Update any existing fixed span-count assertion to reflect the intentional added requests.
