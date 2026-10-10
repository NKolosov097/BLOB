# Implementation Plan: Stage 1B Identity and Sessions

**Branch**: `002-identity-sessions` | **Date**: 2026-10-09 | **Spec**: [spec.md](spec.md)

**Status**: Working implementation plan accepted on 2026-10-10 through user continuation after review. The defaults below are the implementation baseline; providers, credentials, finite live spending caps and public domains remain deployment inputs. Phase 0/1 artifacts are complete; each increment still requires executable tasks, analysis and verification.

## Summary

Deliver every 1B method and session/security journey using the existing Go modular monolith, PostgreSQL and Nuxt boundary. Extend the accepted foundation rather than introducing a second auth service, database abstraction or HTTP client. Export/delete and moderation remain 1F.

Sources: [architecture](../../docs/specs/2026-09-14-blob-mvp-design.md), [1A plan](../../docs/plans/2026-09-14-blob-stage-1a-platform-foundation.md), [recorded 1A verification](../../docs/verification/stage-1a.md).

## Technical Context

**Language/Version**: Existing pinned Go 1.27.1, Node 24.21.0, pnpm 11.26.0, Nuxt 4.5.2 and TypeScript. Do not update versions as incidental work.

**Primary Dependencies**: Existing net/http, pgx/sqlc, goose, oapi-codegen, generated TypeScript client, OpenTelemetry. Proposed OIDC/WebAuthn/TOTP/Argon2id library choices and primary sources are recorded in [research.md](research.md); exact releases are pinned and checked during implementation, not invented here. No custom cryptographic primitives.

**Storage**: PostgreSQL is authoritative for accounts, identities, sessions, challenge budgets and jobs. No Redis in Stage 1. Use transaction-compatible generated queries, database uniqueness and row locking for concurrent state changes.

**Testing**: Go domain/real-PostgreSQL integration/contract tests; existing Vitest and production SSR tests; add a small real-browser authentication suite. No artificial failing tests solely to satisfy TDD.

**Target Platform**: Linux application containers; same-origin browser application; current Chrome, Edge, Firefox and Safari; Russian/English/Arabic RTL.

**Performance Goals**: Bounded request/worker work and tested abuse limits. Benchmark password hashing on a production-class instance before fixing parameters. No fabricated latency SLO or 1,000-user result; full load proof belongs to Stage 5.

**Constraints**: One developer; existing low-cost operating goal; no provider spending in this task. Private data and secrets never enter logs/traces/shared caches.

**Scale/Scope**: Full 1B only. Ordinary devices here are session associations, not E2EE key identities.

## Constitution Check

| Gate | Result |
| --- | --- |
| Specification before implementation; confirmed choices distinct from proposals | Satisfied; prepare scoped tasks and analyze before implementation |
| Entire 1B scope preserved | Satisfied: OIDC, phone OTP, 2FA, passkeys, recovery, sessions/devices all remain |
| Existing module/API/data boundaries; smallest complete change | Satisfied in accepted design |
| Real integration and browser-security evidence | Planned, not executed for 1B |
| Stage 1A acceptance prerequisite | Accepted using original full-stack smoke plus successful exact-HEAD CI and current local follow-up; see [follow-up evidence](../../docs/verification/stage-1a-followup.md) |
| Detailed research/contracts/data model before task generation | Design complete; scoped task consistency analysis required before each implementation increment |

Recheck after Phase 1 design. No constitution exception is requested.

## Project Structure

### Documentation (this feature)

Current: [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [endpoint contract](contracts/identity.md), [quickstart.md](quickstart.md), and [requirements checklist](checklists/requirements.md). Prepare scoped tasks from the accepted design and analyze them before implementation; [increment 01](increments/01-sessions.md) defines the first backend slice.

### Source Code (repository root)

| Surface | Expected scope |
| --- | --- |
| `api/openapi.yaml` | Authoritative identity/session/challenge/method contract and safe errors |
| `server/internal/modules/identity/` | Account, credential, session, MFA and recovery use cases |
| `server/migrations/`, `server/queries/`, `server/internal/db/` | Identity tables/constraints/queries and regenerated sqlc output |
| `server/internal/transport/httpapi/` | Generated interfaces plus handlers and auth/CSRF integration |
| `server/internal/app/`, `server/internal/platform/config/` | Composition, validated secrets/origins/limits and provider configuration |
| `server/internal/modules/system/jobs.go` | Minimal handler-registration seam and safe known job-kind telemetry |
| `web/app/pages/`, `web/app/composables/`, `web/server/utils/` | Authentication/settings UI and request-scoped authenticated SSR |
| `web/app/api/generated/`, `web/i18n/locales/`, `web/test/` | Generated client, three locales and focused verification |
| `Caddyfile`, `compose.yaml`, `.env.example`, CI/scripts | Trusted local HTTPS, safe configuration and browser checks as required |

Do not read or modify `frontend/` or `backend/`. Generated code is regenerated, never edited manually. No speculative file/class inventory beyond the needed module boundaries.

## Observed Foundation Gaps

1. The current OpenAPI contract and Go handler expose health only; no Identity table or module exists.
2. `useBlobApi` is GET-only. `blobApiFetch` currently forwards only traceparent and its test forbids cookies. Add a request-scoped authenticated path that forwards only the required first-party cookie to the fixed internal API; never place per-user headers on a shared generated-client singleton. Preserve anonymous health behavior.
3. `Jobs.handlers` is private and only `system.noop` exists. Expose the smallest registration/composition seam for delivery handlers. Enqueue through the same transaction as the challenge/account change. A payload must not contain a plaintext password or long-lived authentication secret.
4. Worker completion currently keys only by job ID; its fixed five-minute lease has no fencing token. Add a claim token and require it for completion/failure, with bounded handler/provider timeouts. A stale worker cannot complete another worker's claim. Provider idempotency and ambiguous outcomes are handled separately as described in [research.md](research.md).
5. Local routing is HTTP on port 8088. Define trusted local HTTPS before cookie/passkey browser acceptance; do not weaken production cookie rules to make tests pass.
6. Original 1A evidence records host-tooling limitations and passing container substitutes. Current follow-up confirms successful CI for exact source HEAD `9d97aa7cef616988dd24149c54fc1f58982e971c` on 2026-09-28 and local web verification on 2026-10-09. See [follow-up evidence](../../docs/verification/stage-1a-followup.md); the full-stack outage smoke remains historical evidence, not a newly rerun check.

## Inherited Security Design

| Spec | Required plan behavior |
| --- | --- |
| FR-002/003 | Google authorization-code flow with verified issuer/subject, state/nonce, PKCE and fixed callback; unique ownership constraints; no email merge; transaction-safe last-method protection |
| FR-004 | Argon2id, benchmarked parameters, compromised-password rejection; validate lengths before expensive work; parameterized SQL |
| FR-005/006/007 | Purpose/owner-bound random challenges, atomic attempt/consume updates; encrypted TOTP secrets, hashed recovery codes; validated WebAuthn origin/RP/challenge/user verification; no home-grown protocol |
| FR-008 | Separate password reset from factor-loss recovery. Recovery must preserve MFA and revoke sessions; old-channel alerts are queued transactionally. No support bypass for total strong-factor loss |
| FR-009/010 | Random opaque tokens, hash only in PostgreSQL, no browser JWT; `__Host-` cookie with Secure/HttpOnly; rotation and current-state checks; owner-only session/device operations |
| FR-011 | CSRF token plus exact Origin for mutations, exact trusted CORS origins, no state-changing GET, request/body/header limits |
| FR-012/015 | PostgreSQL-backed multi-key challenge/abuse budgets; bounded worker/provider retries; SMS cost reservation/cap/kill switch; recheck live challenge state before sending |
| FR-013 | Server-owned fields, request allowlists, shared actor/action/current-state checks within Identity; narrow interfaces for later bans/deletion/strong reauthentication |
| FR-014/016 | `no-store` for personalized API/SSR data; no secrets in hydration; safe error envelope; allowlist telemetry canaries; output escaping and nonce/hash CSP on HTML, not JSON API |

OpenAPI remains the API source of truth. UUIDv7 identifiers come from Go; timestamps are UTC ISO 8601; lists are stably ordered. Apply create-idempotency and edit-conflict rules deliberately: authenticated create keys bind user/operation/body hash; challenge operations must define a pre-authentication subject rather than pretend an anonymous user exists.

OIDC callback design: GET only renders a noncached completion page without changing account/session state; that page submits the code/state via same-origin CSRF-protected POST, where exchange and session creation occur. Verify provider/browser compatibility in the OIDC increment. Never allow callback query values into logs, tracing, referers or third-party requests. This design preserves the no-mutation-on-GET rule.

## Working Policy Defaults

These defaults were accepted as the working implementation baseline through user continuation on 2026-10-10. They were not specified numerically in the original architecture. Confirmed decisions in spec.md take precedence; external provider/domain configuration is not permission to spend or deploy.

| Topic | Working baseline |
| --- | --- |
| Ordinary MFA and signup | Optional MFA for ordinary users; email/password, Google or phone creates the account; passkeys enroll on an existing account; name/username deferred to 1C |
| First strong factor | Fresh verified-method reauthentication, then proof of new factor. Persist enrollment/recovery state so losing or replacing factors cannot reset the bootstrap exception |
| Strong step-up | Five-minute recency, bound to session and purpose; existing TOTP/passkey required after bootstrap. Fresh first-factor proof PLUS a recovery code authorizes only a restricted factor-replacement journey; a code alone grants neither login nor strong step-up |
| Sessions | Seven-day idle, thirty-day absolute lifetime; no separate remember-me option; absolute deadline preserved during rotation. Logout all includes current session; credential recovery revokes all |
| Cookie | `__Host-blob_session`, Path=/, no Domain, Secure, HttpOnly, SameSite=Lax; verify Google flow compatibility |
| Challenges | Email verification: fifteen minutes; phone OTP: five minutes; password recovery: fifteen minutes; OIDC/WebAuthn transaction: five minutes |
| Code budgets | Six-digit OTP, five failures per purpose/target challenge family; sixty-second resend cooldown, five sends/hour and ten/day per target; resend replaces previous code without restoring attempts |
| Broader throttling | Initial sign-in budget ten failures per account per fifteen minutes and fifty per IP per fifteen minutes; bounded challenge creation per IP. Review denial-of-service impact and proxy trust before enabling |
| Password policy | Fifteen through 128 Unicode code points for every password; no trimming or silent truncation. Use the Pwned Passwords padded range API with a hash prefix only, bounded timeout, local suffix comparison and no sensitive lookup telemetry; fail closed for new passwords when unavailable. Prefix disclosure is the recorded privacy tradeoff |
| TOTP/recovery codes | TOTP thirty-second step with one adjacent step tolerance; reject reuse of an accepted step. Ten high-entropy recovery codes shown once; regeneration invalidates previous codes |
| Passkeys | Discoverable credentials with user verification required; allow synced passkeys and security keys; no attestation-based device restriction |
| Recovery | Unprotected password accounts may reset through verified email; protected accounts also prove a remaining strong factor or, together with fresh first-factor proof, a single-use recovery code. Code-based recovery opens only a browser-bound replacement context; prove the replacement, revoke old sessions and require fresh sign-in before ordinary access |
| Devices | Random browser installation association, no fingerprinting; current-device label, coarse browser/OS and created/last-seen times; no precise location/raw IP in device UI; revocation ends all associated sessions, fresh verified login remains possible |
| Alerts | Successful recovery, password change, strong-factor change and identity link/unlink notify previously verified channels; no secrets in the alert. Failure is retried and surfaced operationally, never weakens the security change |
| Retention | Purge consumed/expired challenge secrets promptly; challenge metadata twenty-four hours, revoked/expired sessions and completed delivery metadata thirty days; inactive device cleanup after ninety days. No raw IP beyond the active abuse window; security-event retention needs explicit review |
| SMS/provider policy | No provider is selected. SMS disabled until credentials, allowed regions, sender and a finite spend cap are configured; no trial purchase or live sends in this task |

Email normalization, Google verified-email treatment, phone-number normalization and identifier uniqueness are specified in the detailed data model. Working normalization: canonical international phone numbers; email domain case normalization without provider-specific dot/plus rewriting; no cross-provider account merging. Record display values separately from comparison keys only where necessary.

## Delivery Increments

| Increment | Deliverable | Required verification |
| --- | --- | --- |
| 1 | Identity/session contract, schema, current-user state, CSRF/origin and session rotation/revocation | Real-DB concurrency, cookie/CSRF browser cases, owner isolation |
| 2 | Email/password registration, verify-before-access, login and reliable email challenges | Password boundaries, duplicate registration, resend/consume races, provider failures |
| 3 | TOTP, recovery codes, first-factor bootstrap, MFA and strong reauthentication | Enrollment proof, code replay, restricted intermediate access, rotation |
| 4 | Passkey enroll/sign-in/remove | Virtual authenticator plus supported-browser checks, wrong RP/origin/account, replay |
| 5 | Google OIDC and safe method link/unlink | Same-email collision, state/nonce/replay, ownership and concurrent last-method tests |
| 6 | Phone-only OTP and SMS limits | Concurrent budgets, replay, retries and spend-cap exhaustion |
| 7 | Complete recovery and security/settings/session UI | Total-factor-loss warning, no recovery bypass, old-channel alerts, revoke all/devices |
| 8 | Convergence across methods, locales and environments | Register -> verify -> login -> MFA -> logout/revoke; all spec outcomes and generation/migration gates |

Each increment retains the approved final scope; partial delivery is not Stage 1B completion. Create small executable tasks from the accepted policies and detailed contracts. Implementation follows the finished specification and returns newly discovered ambiguity to planning.

## Implementation Gates and Validation

- Use the recorded working defaults, including a retained strong factor once protected, finite device/list limits, conservative identifier syntax and restricted recovery authority; do not invent alternatives during implementation.
- [Research](research.md) records source-backed library/protocol choices, padded password lookup, safe callback completion, worker fencing and delivery handling. Validate exact dependency pins/licenses/security during implementation.
- [Data model](data-model.md) specifies records, constraints, normalization, lifecycle transitions, concurrent budgets, bootstrap, restricted recovery and revocation handoff.
- [Contract](contracts/identity.md) specifies endpoint authentication level, CSRF/origin, errors, cache, idempotency/conflict semantics and UI surfaces. Transcribe only the current increment into the existing OpenAPI source.
- [Quickstart](quickstart.md) gives actual repository verification commands and acceptance scenarios. No 1B command result is claimed before implementation.
- Local deterministic delivery is the development baseline. Live email/SMS providers, credentials, finite spending caps and trusted public/RP domains remain external configuration gates; no purchase or live send is authorized here.
- Stage 1G restore work must prevent restored session/challenge or revoked-factor authority from returning. Identity provides current-state/revocation seams; protected journal storage belongs to that stage.
- Produce executable tasks and run spec <-> plan <-> tasks consistency analysis before implementation; converge against all FR/SC IDs after implementation.
No 1B tests have run because there is no implementation. No provider, benchmark, browser compatibility or security-compliance claim is made by this plan.