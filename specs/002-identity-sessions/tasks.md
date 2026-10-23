# Tasks: Stage 1B Identity and Sessions

**Input**: [spec.md](spec.md), [plan.md](plan.md), [increments/01-sessions.md](increments/01-sessions.md), [increments/02-email-password.md](increments/02-email-password.md), [contracts/identity.md](contracts/identity.md), [data-model.md](data-model.md), [research.md](research.md)

**Status**: T001–T017 complete for the first backend slice; see increments/01-convergence.md for review and verification evidence. The later backlog has been corrected against [analysis.md](analysis.md) findings A01–A08 and is now structurally executable, but each phase still requires its own scoped brief, analysis gate and verification before implementation. SMS/live email providers and public auth domains remain external deployment gates (see plan.md "SMS/provider policy" and "Live delivery" notes) — do not enable live sends or claim provider/browser acceptance until those gates are explicitly satisfied outside this repo.

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

## Phase 2: Increment 2 — Email/Password Registration, Login and First Usable Journey (US1, US5)

Follows [increments/02-email-password.md](increments/02-email-password.md) exactly. Delivers the first journey a person can complete in a browser. Email UI, its three locales and the delivery infrastructure every later method reuses belong here, not in phases 7–8 (finding A06).

### Delivery infrastructure (prerequisite for every challenge-sending method, finding A05)

- [ ] T056 (US1) Add worker fencing and a handler-registration seam: `claim_token` in `server/migrations/00003_jobs_claim_token.sql`, claim/complete/fail queries in `server/queries/system.sql`, regenerated `server/internal/db/*`, exported registration in `server/internal/modules/system/jobs.go`, wiring in `server/internal/app/worker.go`. Completion and failure require a matching live claim; handler execution is bounded below the lease
- [ ] T057 (US1) Add `auth_deliveries` per data-model.md: part of the shared `server/migrations/00004_identity_email.sql`, queries in `server/queries/identity_delivery.sql`, encrypted destination/body material with key version, unique `provider_key`, pending/sending/sent/failed/unknown/cancelled statuses, cost reservation before any external effect, secret erasure at consumption/expiry/terminal processing. Jobs carry only the delivery ID. Local deterministic delivery is the development provider
- [ ] T062 (US1) Add `auth_requests` idempotency per data-model.md: part of the shared `server/migrations/00004_identity_email.sql`, queries and the helper in `server/internal/modules/identity/idempotency.go`. Pre-authentication keys bind browser context plus purpose/target digest; authenticated keys bind user/operation/body HMAC. Store references, never secret response bodies

### Backend

- [ ] T018 (US1) Extend `api/openapi.yaml` with register, login, verify-email and resend endpoints per contracts/identity.md
- [ ] T019 (US1) Create `server/migrations/00004_identity_email.sql` with `login_identities`, `auth_challenges`, `auth_buckets`, `auth_deliveries` and `auth_requests` per data-model.md, up/down in dependency order. Do not add email or password columns to `users`; `users` gains only the access/protection/security fields the model already names (finding A03)
- [ ] T020 (US1) Add queries to `server/queries/identity_email.sql` for account creation, `login_identities` lookup by comparison key, and atomic `auth_challenges` issue/consume with the `auth_buckets` charge in one transaction; regenerate `server/internal/db/*`
- [ ] T021 (US1) Implement Argon2id hashing with benchmarked parameters and the Pwned Passwords padded-range check in `server/internal/modules/identity/password.go`, failing closed for new passwords, per research.md and plan.md "Password policy"
- [ ] T022 (US1) Implement email-verification challenge issuance in `server/internal/modules/identity/email.go`: challenge creation, bucket charge, `auth_deliveries` record and job enqueue commit in one transaction; resend cancels unsent deliveries, keeps the family attempt budget and invalidates the prior proof
- [ ] T023 (US1) Implement the login use case in `server/internal/modules/identity/email.go`: credential verification, `access_state` gate (`pending_email` blocks full access), then `identity.Sessions.Issue`. Session proof metadata is not added here; it arrives with T063 in increment 3
- [ ] T024 (US1) Implement handlers in `server/internal/transport/httpapi/identity_email.go` for register/login/verify-email/resend with generic anti-enumeration responses and the existing safe-error envelope; regenerate `api.gen.go` and `web/app/api/generated/*`
- [ ] T025 (US1) Integration and HTTP tests: duplicate registration cannot replace password material, unverified-access denial, resend/consume races, stale-worker completion rejected by the claim token, ambiguous provider outcome leaves the code invalid, bounded delivery retry, password length and compromised-password boundaries, idempotent replay returns a safe completed/conflict result without reproducing a session token

### Usable journey

- [ ] T058 (US1) Add request-scoped authenticated SSR in `web/server/utils/`: forward only the first-party session cookie to the fixed internal API, never on the shared generated-client singleton; preserve anonymous health behavior in `web/app/composables/useBlobApi.ts`
- [ ] T059 (US1, US5) Build `web/app/pages/` sign-up, sign-in and email-verification pages with Russian, English and Arabic strings in `web/i18n/locales/`; verify Arabic RTL layout and keyboard/screen-reader behavior for success, failure and expiry states
- [ ] T060 (US1) Define trusted local HTTPS in `Caddyfile`/`compose.yaml` so `__Host-` cookies work in a real browser; do not weaken production cookie rules for tests
- [ ] T061 (US1, US5) Real-browser smoke over that origin: register → verify → login → logout, in all three locales
- [ ] T063a (US1) Run the spec↔plan↔tasks analysis gate for this increment before implementation, and record convergence in `specs/002-identity-sessions/increments/` afterwards, including generation-drift, migration round-trip, `go vet`/`build`, web typecheck/build and `docker compose config --quiet`

## Phase 3: Increment 3 — TOTP, Recovery Codes, Bootstrap, MFA Gating and Strong Step-Up (US3)

Introduces the shared bootstrap and step-up machinery that passkeys in phase 4 also depend on.

- [ ] T026 (US3) Migration and queries for `totp_credentials` (encrypted secret, key version, status, `last_accepted_step`) and `recovery_codes` (set id, hashed, single-use) per data-model.md; regenerate `server/internal/db/*`
- [ ] T063 (US3) Add MFA-pending gating and session proof metadata (finding A07): an MFA-pending `auth_challenges` purpose, session `strong_at`/`strong_method_id`/`strong_purpose`/`proof_security_version`, and the rule that a first factor alone never issues a full protected-account session. Adapt the T023 login path to call `Issue` only after all required proof; the same gate applies to Google (phase 5) and phone (phase 6) login
- [ ] T027 (US3) Implement TOTP enroll/confirm/verify in `server/internal/modules/identity/totp.go`: thirty-second step, one adjacent step tolerance, atomic accepted-step update rejecting reuse
- [ ] T028 (US3) Implement recovery-code generation and consumption in `server/internal/modules/identity/recovery_codes.go`: ten high-entropy codes shown once, atomic unused-to-used transition, regeneration invalidating the previous set
- [ ] T029 (US3) Implement first-factor bootstrap and strong step-up in `server/internal/modules/identity/stepup.go` using `users.protection_state` and `security_version`, with the five-minute purpose-bound recency window per plan.md
- [ ] T030 (US3) Add OpenAPI schemas and handlers in `server/internal/transport/httpapi/identity_mfa.go` for MFA enroll/confirm/verify and step-up; regenerate Go and TypeScript artifacts
- [ ] T064 (US3, US5) Build MFA enrollment and challenge UI in `web/app/pages/` with the three locale files; RTL, keyboard and screen-reader pass for challenge success, failure and expiry
- [ ] T031 (US3) Integration tests: bootstrap usable once per account and not resettable by deletion, loss reports or disabling MFA; last working method retained under concurrent removal; step-up recency and purpose binding; accepted-step and recovery-code replay rejection; an MFA-pending context cannot call ordinary authenticated endpoints
- [ ] T065a (US3) Analysis gate before implementation and convergence record afterwards, with the same generation, migration and build checks as T063a

## Phase 4: Increment 4 — Passkeys (US3)

Depends on the phase 3 bootstrap and step-up machinery (T029, T063); these phases are not independent (finding A08).

- [ ] T032 (US3) Migration and queries for `passkeys` per data-model.md: globally unique `credential_id`, immutable account `user_handle`, sign count, transports, backup flags; regenerate `server/internal/db/*`
- [ ] T033 (US3) Implement enroll/authenticate/remove in `server/internal/modules/identity/passkeys.go` with the pinned WebAuthn library from research.md: discoverable credentials, user verification required, no attestation-based device restriction
- [ ] T034 (US3) Add OpenAPI schemas and handlers in `server/internal/transport/httpapi/identity_passkeys.go`, wired through the FR-003 reauthentication and last-method rules; regenerate Go and TypeScript artifacts
- [ ] T065 (US3, US5) Build passkey enrollment, sign-in and removal UI with the three locale files; the cancelled-ceremony fallback to another method is reachable by keyboard
- [ ] T035 (US3) Virtual-authenticator and supported-browser tests: wrong RP, wrong origin, wrong account, replayed and expired challenge, cancelled ceremony fallback
- [ ] T066a (US3) Analysis gate before implementation and convergence record afterwards, with the same generation, migration and build checks as T063a

## Phase 5: Increment 5 — Google OIDC and Method Link/Unlink (US1, US3)

- [ ] T036 (US1) Extend `login_identities` for Google: the verified canonical `(issuer, subject)` pair is unique **globally across all accounts**, not per account (finding A04). A link collision must fail rather than create a second owner. Do not create a separate `identities` table
- [ ] T037 (US1) Implement the authorization-code flow in `server/internal/modules/identity/oidc.go` with PKCE, state and nonce; the GET callback renders a no-mutation completion page that posts code and state same-origin under CSRF, where exchange and session creation occur, per plan.md "OIDC callback design". Callback query values never reach logs, traces, referers or third-party requests
- [ ] T038 (US1, US3) Implement link and unlink in `server/internal/modules/identity/methods.go` enforcing FR-003 reauthentication and last-method protection under an account lock; no email-based account merge
- [ ] T039 (US1) Add OpenAPI schemas and handlers for the OIDC callback and method link/unlink; regenerate Go and TypeScript artifacts
- [ ] T066 (US1, US5) Build Google sign-in and linked-methods UI with the three locale files
- [ ] T040 (US1, US3) Tests: same-email non-merge, global issuer/subject ownership collision, state and nonce replay or tamper rejection, crash or ambiguous exchange invalidating the ceremony, concurrent last-method removal, MFA gate applied to Google login
- [ ] T067a (US1) Analysis gate before implementation and convergence record afterwards, with the same generation, migration and build checks as T063a

## Phase 6: Increment 6 — Phone OTP and SMS Limits (US1)

- [ ] T041 (US1) Add phone support to `login_identities` (canonical international comparison key) and OTP budget fields to the existing `auth_challenges`/`auth_buckets` records. Do not add a phone column to `users` and do not create a parallel challenge schema (finding A03)
- [ ] T042 (US1) Implement phone OTP send and verify in `server/internal/modules/identity/phone.go` reusing the T057 delivery path: resend cooldown, attempt limits, transactional spend-cap reservation and an operational kill switch. SMS stays disabled pending the provider gate
- [ ] T043 (US1) Add OpenAPI schemas and handlers for phone-only signup and login; regenerate Go and TypeScript artifacts
- [ ] T067 (US1, US5) Build phone sign-up and OTP challenge UI with the three locale files
- [ ] T044 (US1) Tests: concurrent budget consumption, OTP replay, bounded delivery retry, spend-cap exhaustion with the provider disabled or mocked, MFA gate applied to phone login
- [ ] T068a (US1) Analysis gate before implementation and convergence record afterwards, with the same generation, migration and build checks as T063a

## Phase 7: Increment 7 — Recovery, Alerts and Security Settings UI (US2, US4)

- [ ] T045 (US4) Implement recovery in `server/internal/modules/identity/recovery.go`: unprotected accounts reset through the verified email channel; protected accounts prove a remaining strong factor, or fresh first-factor proof plus one recovery code, which opens only the browser-bound `replacement_required` context per data-model.md
- [ ] T046 (US4) Wire recovery completion to the `security_version` increment, full session revocation and transactionally enqueued old-channel alerts over the T057 delivery path; alert failure is retried and surfaced operationally and never weakens the security change
- [ ] T047 (US4) Add OpenAPI schemas and handlers for recovery initiate/confirm and the restricted replacement context, which cannot export, link identities, change unrelated settings or call ordinary account endpoints; regenerate Go and TypeScript artifacts
- [ ] T048 (US2, US4) Build the security settings surface in `web/app/pages/`: session and device list with revoke one, revoke device and revoke all, recovery journeys and recovery-code display, over the T058 authenticated SSR path; add the three locale files
- [ ] T049 (US2, US4) Tests: total strong-factor loss warns and offers no bypass, a consumed code with abandoned setup stays consumed, alert delivery and retry, revoke-one/revoke-all/device-revoke flows end exactly the right sessions
- [ ] T069a (US4) Analysis gate before implementation and convergence record afterwards, with the same generation, migration and build checks as T063a

## Phase 8: Increment 8 — Cross-Flow Convergence (US5)

Per-increment UI, locales and accessibility are delivered in phases 2–7; this phase proves them together.

- [ ] T050 (US5) Audit `web/i18n/locales/` for completeness across every auth journey and confirm Arabic RTL layout on the combined surfaces
- [ ] T051 (US5) Cross-flow keyboard-navigation and screen-reader pass covering challenge success, failure and expiry states for all five stories
- [ ] T052 (US5) Real-browser end-to-end suite: register → verify → login → MFA → logout/revoke across supported browsers and all three locales
- [ ] T053 (US5) Produce the FR/SC convergence record in `docs/verification/` mapping evidence to FR-001…FR-016 and SC-001…SC-006
- [ ] T055 (US5) Final cross-increment verification: generation drift check, full migration round-trip, `go vet`/`build`, web typecheck and production build, `docker compose config --quiet`; record the live SMS/email-provider and public-domain gates as still pending

## Dependencies

- Phase 1 blocks every later phase (session issuance, authentication, cookies and CSRF are shared infrastructure).
- Within phase 2, T056/T057/T062 precede T022–T025; T058 precedes T059 and T061; T060 precedes T061.
- Phase 2 blocks phases 3–7: the first verified sign-in method and the delivery and idempotency infrastructure are prerequisites.
- Phase 3 blocks phase 4. Passkeys reuse the bootstrap, step-up and MFA-gating machinery from T029 and T063, so phases 3 and 4 are not independent.
- Phase 5 depends on phase 2 and reuses the phase 3 step-up rules. Phase 6 depends on phase 2 and on the T057 delivery path, and is independent of phases 3–5.
- Phase 7 depends on phases 2–6. Phase 8 depends on all prior phases being functionally complete.

## Notes

- No task may be checked off until its real-database or real-browser verification has actually run, per its phase and increments/01-sessions.md "Verification Required Before Completion".
- Do not enable live SMS or email sending, and do not claim public-domain or browser compatibility, until the external provider and domain gates in plan.md are satisfied outside this task list.
- Every phase runs its own analyze-before-build gate (the `a`-suffixed task in each phase) against contracts/identity.md and data-model.md, then records convergence, following the process used for increment 1. This replaces the former single process task T054.
- Task IDs are not contiguous: the original T018–T055 numbering is kept for traceability with analysis.md, and tasks added by findings A05–A08 use T056 and above. Execution order is the listed order within each phase.
