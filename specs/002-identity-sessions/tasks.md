# Tasks: Stage 1B Identity and Sessions

**Input**: [spec.md](spec.md), [plan.md](plan.md), [increments/01-sessions.md](increments/01-sessions.md), [contracts/identity.md](contracts/identity.md), [data-model.md](data-model.md), [research.md](research.md)

**Status**: T001–T017 complete for the first backend slice; see increments/01-convergence.md for review and verification evidence. T018–T055 remain non-executable backlog pending scoped correction and analysis (see analysis.md). SMS/live email providers and public auth domains remain external deployment gates (see plan.md "SMS/provider policy" and "Live delivery" notes) — do not enable live sends or claim provider/browser acceptance until those gates are explicitly satisfied outside this repo.

Tasks are grouped by delivery increment (plan.md "Delivery Increments"). Increment 1 is the first backend session slice and follows [increments/01-sessions.md](increments/01-sessions.md) precisely. Later increments retain full 1B scope (all methods, MFA, passkeys, recovery, devices, UI, i18n) per spec.md and plan.md; detailed invariants are linked rather than repeated here.

## Phase 1: Increment 1 — Session Foundation (Foundational; underlies US1–US4)

Follows increments/01-sessions.md exactly. No login/registration route exists yet; `Issue`/`Rotate` are trusted in-process use cases only.

- [x] T001 Add the eight session/device/context endpoints and schemas to `api/openapi.yaml` per increments/01-sessions.md "Eight HTTP Endpoints"
- [x] T002 Create `server/migrations/00002_identity_sessions.sql` with `users`/`devices`/`sessions` tables per increments/01-sessions.md "Minimum Persistence" (up/down in dependency order)
- [x] T003 Add named queries to `server/queries/identity_sessions.sql` matching the exact list in increments/01-sessions.md
- [x] T004 Regenerate sqlc output into `server/internal/db/*`
- [x] T005 Add `server/internal/platform/config/identity.go` + `identity_test.go` implementing `LoadIdentity()` (AUTH_PUBLIC_ORIGIN, AUTH_BROWSER_KEY only; fixed session durations live as shared constants in identity, not in config)
- [x] T006 Implement `server/internal/transport/httpapi/browser_security.go`: cookie helpers, anonymous context, CSRF HMAC, exact-Origin check, signed pagination cursor
- [x] T007 Implement `server/internal/modules/identity/sessions.go`: `NewSessions`, `Issue`, `Rotate`, `Authenticate`, `ListSessions`, `ListDevices`, `RevokeSession`, `RevokeDeviceSessions`, `RevokeAllSessions`, `Logout`, injected `now func() time.Time`
- [x] T008 Update `server/internal/transport/httpapi/handler.go` and `server/internal/app/api.go` to the new `NewHandler(health, sessions, identityConfig, middleware...)` signature and compose `identity.Sessions`
- [x] T009 Implement the eight strict handlers in `server/internal/transport/httpapi/identity.go` with centralized safe-error mapping per increments/01-sessions.md "Safe Error and Configuration Contract"
- [x] T010 Regenerate `server/internal/transport/httpapi/api.gen.go`; override `StdHTTPServerOptions.ErrorHandlerFunc` and strict error callbacks without editing generated code
- [x] T011 Update `server/internal/transport/httpapi/handler_test.go` and `server/internal/platform/observability/otel_test.go` for the new constructor, preserving existing assertions
- [x] T012 In `server/internal/app/failure.go`/`process_test.go`, preserve `TestProcessHidesMalformedDatabaseConfig` and add an invalid-auth-key startup canary (`identity_config_invalid`)
- [x] T013 Update `.env.example`, `compose.yaml`, `README.md` with AUTH_PUBLIC_ORIGIN/AUTH_BROWSER_KEY (no committed secret) and the PowerShell key-generation step from increments/01-sessions.md
- [x] T014 Regenerate `web/app/api/generated/*` only; no UI/composable/SSR behavior change
- [x] T015 Write `server/internal/modules/identity/sessions_integration_test.go`: real-DB ownership, idle/absolute expiry via injected clock, concurrent Rotate/Rotate and Rotate/RevokeAll, Authenticate-vs-revocation races
- [x] T016 Write `server/internal/transport/httpapi/identity_test.go` and `browser_security_test.go`: TLS client, Origin/CSRF matrix, logout exception cases, pagination >50 with ties, cursor tamper/owner mismatch, device cap, JSON error envelope, no-store headers
- [x] T017 Run full verification from increments/01-sessions.md item 7 (TEST_DATABASE_URL Go tests, go vet/build, up/down/up migration, double generation with no drift, web typecheck/build, `docker compose config --quiet`); record results, do not claim browser/live-provider acceptance

## Phase 2: Increment 2 — Email/Password Registration and Login (US1)

- [ ] T018 Extend `api/openapi.yaml` with register/login/verify-email/resend endpoints per contracts/identity.md
- [ ] T019 Migration: add password/email columns to `users` and a `challenges` table (purpose, owner, expiry, attempts, consumed) per data-model.md
- [ ] T020 Add queries for user creation, email lookup/normalization, atomic challenge issue/consume
- [ ] T021 Implement Argon2id hashing (benchmarked parameters) and Pwned Passwords padded-range check per research.md and plan.md "Password policy"
- [ ] T022 Implement email-verification challenge issuance with transactional delivery-job enqueue (`server/internal/modules/system/jobs.go` seam)
- [ ] T023 Implement login use case: credential verification, access-state gate (pending_email blocks full access), calls `identity.Sessions.Issue`
- [ ] T024 Implement HTTP handlers for register/login/verify-email/resend with generic anti-enumeration responses (plan.md working defaults)
- [ ] T025 Integration/HTTP tests: duplicate registration, unverified-access denial, resend/consume races, simulated delivery failure, password length/compromised-password boundaries

## Phase 3: Increment 3 — TOTP, Recovery Codes, Bootstrap, Strong Step-Up (US3)

- [ ] T026 Migration: `totp_secrets` (encrypted secret) and `recovery_codes` (hashed, single-use) tables
- [ ] T027 Implement TOTP enroll/confirm/verify (30s step, ±1 tolerance, reject reused accepted step)
- [ ] T028 Implement recovery-code generation (ten codes, shown once) and consumption; regeneration invalidates prior codes
- [ ] T029 Implement strong step-up requirement and first-factor bootstrap tracking via `users.protection_state`, per plan.md "First strong factor"/"Strong step-up"
- [ ] T030 Add OpenAPI schemas/handlers for MFA enroll/verify/step-up endpoints
- [ ] T031 Integration tests: bootstrap usable once per account, last-working-method retained under concurrent removal, step-up recency window, code replay rejection

## Phase 4: Increment 4 — Passkeys (US3)

- [ ] T032 Migration: `webauthn_credentials` table
- [ ] T033 Implement passkey enroll/authenticate/remove using the chosen WebAuthn library (research.md), discoverable credentials + required user verification
- [ ] T034 Add OpenAPI schemas/handlers for passkey ceremonies, wired through FR-003 reauth/last-method rules
- [ ] T035 Virtual-authenticator and supported-browser tests: wrong RP/origin/account, replayed/expired challenge, cancelled ceremony fallback to another method

## Phase 5: Increment 5 — Google OIDC and Method Link/Unlink (US1, US3)

- [ ] T036 Migration: `identities` table keyed by verified issuer+subject, unique per account
- [ ] T037 Implement OIDC authorization-code flow (PKCE, state, nonce) with no-mutation GET completion page and CSRF-protected POST exchange per plan.md "OIDC callback design"
- [ ] T038 Implement link/unlink service enforcing FR-003 reauthentication and last-method protection; no email-based account merge
- [ ] T039 Add OpenAPI schemas/handlers for OIDC callback and method link/unlink
- [ ] T040 Tests: same-email non-merge, state/nonce replay and tamper rejection, concurrent last-method removal, conflicting ownership

## Phase 6: Increment 6 — Phone OTP and SMS Limits (US1)

- [ ] T041 Migration/queries: phone number column with canonical normalization, OTP challenge budget fields per plan.md "Code budgets"
- [ ] T042 Implement phone OTP send/verify with resend cooldown, attempt limits, spend-cap and operational kill switch (SMS stays disabled pending provider gate)
- [ ] T043 Add OpenAPI schemas/handlers for phone-only signup/login
- [ ] T044 Tests: concurrent budget consumption, OTP replay, bounded delivery retries, spend-cap exhaustion behavior with provider disabled/mocked

## Phase 7: Increment 7 — Recovery, Alerts, and Settings/Session UI (US2, US4)

- [ ] T045 Implement recovery service: unprotected-account email reset vs protected-account recovery (remaining strong factor, or code + fresh first-factor) per plan.md "Recovery"
- [ ] T046 Wire recovery completion to session revocation and old-channel security alerts (plan.md "Alerts"), preserving MFA policy
- [ ] T047 Add OpenAPI schemas/handlers for recovery initiate/confirm, restricted post-recovery replacement context
- [ ] T048 Build `web/app/pages/` and `web/app/composables/` for sign-in/up, MFA enrollment, passkeys, recovery, and session/device management (list, revoke one/all), using request-scoped authenticated SSR fetch (`web/server/utils/`)
- [ ] T049 Tests: total-strong-factor-loss warning/no-bypass messaging, alert delivery and retry, revoke-one/revoke-all/device-revoke UI flows

## Phase 8: Increment 8 — Convergence, i18n, and Verification (US5)

- [ ] T050 Add Russian/English/Arabic strings in `web/i18n/locales/` for all auth journeys; verify Arabic RTL layout
- [ ] T051 Keyboard-navigation and screen-reader pass across challenge success/fail/expiry states for all five stories
- [ ] T052 Add a real-browser end-to-end suite covering register → verify → login → MFA → logout/revoke across supported browsers and locales
- [ ] T053 Produce an FR/SC convergence record in `docs/verification/` mapping implementation evidence to FR-001…FR-016 and SC-001…SC-006
- [ ] T054 Run spec↔plan↔tasks consistency analysis before starting each increment's implementation work (process gate, repeat per increment)
- [ ] T055 Final cross-increment verification: generation drift check, full migration round-trip, go vet/build, web typecheck/build, `docker compose config --quiet`; explicitly record live SMS/email-provider and public-domain gates as still pending, not complete

## Dependencies

- Phase 1 blocks every later phase (session issuance/auth/cookies/CSRF are shared infrastructure).
- Phase 2 blocks Phases 3–7 (first verified sign-in method needed before MFA/passkeys/linking/recovery can be exercised end-to-end).
- Phases 3 and 4 are independent of each other but both depend on Phase 2; Phase 5 depends on Phase 2 and reuses Phase 3/4 step-up rules.
- Phase 6 depends on Phase 2 (shares challenge/budget infrastructure) but is independent of Phases 3–5.
- Phase 7 depends on Phases 2–6 (recovery must integrate every enrolled method; UI surfaces session/device/MFA/passkey/recovery controls built earlier).
- Phase 8 depends on all prior phases being functionally complete.

## Notes

- No task in this list may be checked off until its real-DB/real-browser verification (per its phase and increments/01-sessions.md "Verification Required Before Completion") has actually run.
- Do not enable live SMS/email sending or claim public-domain/browser compatibility until the external provider and domain gates in plan.md are satisfied outside this task list.
- Every phase beyond Increment 1 must produce its own scoped sub-tasks against contracts/identity.md and data-model.md before implementation, following the same analyze-before-build gate used for Increment 1.