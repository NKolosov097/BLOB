# Increment 01: Session Foundation Convergence

Date: 2026-10-10. Status: first-slice source review and verification accepted. Scope is [01-sessions.md](01-sessions.md), with corrections tracked in [01-session-review.md](01-session-review.md). This record does not accept all of Stage 1B.

## Accepted Behavior

The backend slice implements the eight context/current-user/session/device endpoints, internal trusted session issuance and rotation, PostgreSQL ownership constraints, hashed opaque tokens, fixed idle/absolute deadlines, owner-scoped pagination and revocation, and atomic current-token rechecks under owner locks. It has no public account creation or login endpoint.

Browser transport uses host-only Secure/HttpOnly/SameSite cookies, a signed anonymous context, session/context-bound CSRF, exact configured Origin checks, safe JSON errors and no-store responses. Guards run before generated parameter parsing; malformed paths and HEAD requests follow safe routing. API startup requires validated origin/key configuration; the worker does not receive the signing key. Existing database startup error semantics remain intact.

R01–R12 are corrected in source. T01–T09 test corrections are present and meaningful: fixed-clock expiry boundaries, independent revocation authorizer, pagination ties, real Issue rollback, an observed database-lock barrier for logout/rotation overlap, actual signed HTTP continuation, device/all revocation, expired/duplicate-cookie and rotated-CSRF cases, and identity-secret telemetry canaries. No unresolved code blocker was identified in final review.

This covers the first-slice portions of FR-009/010/011 and the ownership/current-state and safe-transport portions of FR-013/014. It does not claim complete user-facing acceptance of those requirements or unrelated authentication methods.

## Verification Evidence

| Check | Evidence/status |
| --- | --- |
| Real PostgreSQL service tests | Final uncached aggregate run passed; identity package 11.023s, no skips |
| Real PostgreSQL HTTP/browser-helper tests | Final uncached aggregate run passed; HTTP API package 9.879s, no skips |
| Trusted TLS transport | Tests use `httptest.NewTLSServer` and its trusted HTTP client; real database fixtures, no public test-login route |
| Final full Go tests, vet and build | `go -C server test ./... -count=1`, `go vet ./...`, and `go build ./...` passed with exit 0 against migrated disposable PostgreSQL (`blob_migration_roundtrip`, `CI=true`, `TEST_DATABASE_URL` set); no test skips |
| Migration up/down/up | Passed on a separate disposable database; migration SHA256 `6A28EFFFC323404A09D4F1B6B5B81B60AE9ABEF8A0A29C410D56381A2F8F09EC` unchanged |
| Repeated Go and TypeScript generation | Passed with no second-run drift |
| Web typecheck and production build | Passed against regenerated API artifacts |
| Compose configuration | Passed without `.env` and with an ephemeral signing key; auth configuration is API-only |
| Whitespace verification | Final `git diff --check` passed with exit 0 |

The final full run also passed process/startup tests (39.483s), system (4.998s), config (0.520s), database (2.464s), HTTP server (2.914s), observability (9.901s) and request ID (0.781s). No application code changed after that run.

The pinned generated API code now imports `github.com/oapi-codegen/runtime` v1.7.0 for parameter binding, with its jsonmerge dependency. UUID v1.6.0 is promoted from indirect to direct use. No existing dependency version was upgraded; these additions support the generated contract rather than introducing an authentication framework.

## Remaining Scope and Handoff

No real-browser UI, authenticated SSR, public HTTPS deployment, live provider integration, live sends or public login/signup acceptance is claimed. The existing web status page remains the application entry point. Session issuance is available only to trusted in-process callers and test fixtures until the next usable email/password increment.

T001–T017 are complete for this first slice. T018–T055 remain non-executable planning backlog: their known schema/dependency/UI-placement defects in [analysis.md](../analysis.md) must be corrected in a scoped next-increment task review before implementation. OIDC, phone OTP, TOTP, passkeys, recovery, session UI and localized accessible journeys remain within Stage 1B; provider/domain values remain external deployment inputs.
