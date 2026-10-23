# Increment 02: Email/Password Registration, Login and First Usable Journey

Date: 2026-10-23. Status: implementation-ready brief after the phase 2 consistency gate. Follow the [specification](../spec.md), [working plan](../plan.md), [data model](../data-model.md) and [full contract](../contracts/identity.md). This is the second slice of Stage 1B. It builds directly on the accepted [session foundation](01-sessions.md); do not re-decide anything that brief already fixed. Generate and check executable tasks (phase 2 of [tasks.md](../tasks.md)) against this brief before editing application code.

## Outcome and Boundary

Deliver the first journey a real person can complete in a browser: register with an email address and password, receive and consume a verification challenge, sign in, and sign out. Registration and sign-in become public endpoints; `identity.Sessions.Issue` stops being test-only and gains exactly one production caller. Deliver the supporting delivery infrastructure every later challenge-sending method reuses: worker claim-token fencing, a handler-registration seam, an encrypted delivery record and transactional abuse budgets.

Included FR coverage: FR-001 and FR-004 in full; FR-005, FR-012 and FR-015 for the email channel; the email-journey part of FR-016; the remaining SSR/no-store part of FR-014. FR-002/003/006/007/008 stay in later increments: no Google, phone, TOTP, passkey, recovery, method link/unlink or security-settings surface is implemented here. Session and device management UI remains in increment 7; this slice adds only the pages the email journey itself needs.

Two gates stay explicitly open and must not be claimed: no live email provider is configured, and no public domain or publicly trusted certificate exists. Development and test delivery are deterministic local transports. Trusted local HTTPS exists only so `__Host-` cookies behave correctly in a real browser.

## Existing Seams and Exact File Reach

| File | Required change |
| --- | --- |
| `api/openapi.yaml` | Add only the four endpoints below plus `enabled_methods` content for `/auth/context`; explicit CSRF, Idempotency-Key and safe errors |
| `server/migrations/00003_jobs_claim_token.sql` | Add `jobs.claim_token`; up/down leave existing rows valid |
| `server/migrations/00004_identity_email.sql` | `login_identities`, `auth_challenges`, `auth_buckets`, `auth_deliveries`, `auth_requests`; up/down in dependency order |
| `server/queries/system.sql` | Claim/complete/fail take the claim token; no new job framework |
| `server/queries/identity_email.sql` | Named queries listed below |
| `server/queries/identity_delivery.sql` | Delivery claim/transition queries listed below |
| `server/internal/db/*` | Regenerate sqlc; never edit manually |
| `server/internal/modules/system/jobs.go` | Exported handler registration, claim-token fencing, bounded handler timeout, registry-derived safe `job.kind` attribute |
| `server/internal/modules/identity/password.go` | Argon2id hashing/verification and the compromised-password check behind an injectable interface |
| `server/internal/modules/identity/email.go` | Register, resend, verify and password-login use cases |
| `server/internal/modules/identity/delivery.go` | Delivery record creation, claim, terminal transitions and secret erasure |
| `server/internal/modules/identity/buckets.go` | Transactional abuse-budget charge and failure recording |
| `server/internal/modules/identity/idempotency.go` | `auth_requests` helper for pre-authentication and authenticated keys |
| `server/internal/modules/identity/*_integration_test.go` | Real-DB tests per the verification list; no database mocks |
| `server/internal/transport/httpapi/identity_email.go` | Four strict handlers, anti-enumeration shaping, Idempotency-Key parsing |
| `server/internal/transport/httpapi/identity_email_test.go` | HTTP/TLS cases over a real database |
| `server/internal/transport/httpapi/handler.go`, `api.gen.go` | Wire the new service; regenerate OpenAPI output |
| `server/internal/platform/config/identity.go`, `identity_test.go` | Add `AUTH_SECRET_KEY`, optional `AUTH_TRUSTED_PROXY_HEADER`, email transport settings |
| `server/internal/app/api.go`, `worker.go`, `failure.go`, `process_test.go` | Compose the new services, register the delivery handler on the worker, add the invalid-secret-key startup canary |
| `web/server/utils/authenticatedFetch.ts` | Request-scoped SSR fetch forwarding only `__Host-blob_session` |
| `web/app/pages/signup.vue`, `signin.vue`, `verify-email.vue` | The three journey pages |
| `web/app/composables/useBlobApi.ts` | Keep the anonymous health path unchanged; add the authenticated request-scoped path |
| `web/i18n/locales/{ru,en,ar}.json` | Strings for every state of the three pages |
| `web/app/api/generated/*` | Regenerate only |
| `web/test/`, browser smoke | Unit, SSR and real-browser coverage per the verification list |
| `Caddyfile`, `compose.yaml`, `.env.example`, `README.md` | Trusted local HTTPS origin, new configuration names, no committed secret |

Nothing in `frontend/` or `backend/` is read or changed. Generated code is regenerated, never hand-edited.

## Worker Fencing and Handler Registration

`jobs.claim_token uuid` is added nullable, set by `ClaimJob` to a fresh UUIDv7 on every claim, and cleared on terminal transitions. `CompleteJob` and `FailJob` match on `id AND claim_token AND status='running'`; they report whether a row changed, and a zero-row result is a lost claim, logged with the job ID only and treated as success for the stale worker. The existing five-minute lease and `attempts < 5` bound are unchanged. The existing `ponytail:` note about the fixed lease stays accurate and stays in place.

`Jobs` gains `Register(kind string, h Handler) error`, rejecting a duplicate or empty kind, and `NewJobs` keeps registering `system.noop`. Composition in `app/worker.go` registers the delivery handler. The `job.kind` span attribute is derived from the registry (a registered kind is safe to record, anything else stays `unknown`), replacing the hardcoded comparison. Handler execution runs under a context bounded well below the lease; state the chosen bound in the convergence record. Payloads carry only `{"delivery_id": "<uuid>"}`; no address, code, password or any other secret enters a job row.

## Persistence

Create only what this slice uses. `users` already carries `access_state`, `mfa_required`, `protection_state` and `security_version`; do not add email, password or phone columns to it (finding A03).

- `login_identities`: id UUID PK; user_id FK; kind constrained to `email` for now; comparison_key text; password_hash text nullable; verified_at timestamptz nullable; created_at/updated_at; revoked_at nullable. Partial unique index on `(kind, comparison_key) WHERE revoked_at IS NULL`. Issuer/subject and phone columns arrive with their own increments; do not add empty ones now.
- `auth_challenges`: id UUID PK; family_id UUID; browser_binding_hash bytea(32); user_id FK nullable; purpose constrained to `email_verification` for now; target_key_hash bytea(32); proof_digest bytea nullable; attempts integer; created_at; expires_at; consumed_at/cancelled_at nullable. Index by family, by owner and by expiry.
- `auth_buckets`: scope; keyed_subject_digest bytea(32); purpose; window_start; failures; sends; next_send_at; expires_at. Unique `(scope, keyed_subject_digest, purpose, window_start)`.
- `auth_deliveries`: id UUID PK; challenge_id nullable FK; user_id nullable FK; channel (`email`); purpose; encrypted_material bytea; key_version integer; status constrained to pending/sending/sent/failed/unknown/cancelled; provider_key text unique; attempts integer; created_at; expires_at; sent_at nullable. `reservation_amount`/`currency` columns exist and are zero for email; SMS populates them in increment 6.
- `auth_requests`: subject_digest bytea(32); operation; idempotency_key_digest bytea(32); body_hmac bytea(32); result_reference; status; created_at; expires_at. Unique `(subject_digest, operation, idempotency_key_digest)`.

All IDs are UUIDv7 from the existing library; timestamps are UTC. The down migration drops in reverse dependency order and never touches `users`, `devices`, `sessions` or `jobs` rows.

Named query responsibilities: `InsertUser`; `LockUserByID`; `FindActiveEmailIdentity`; `InsertEmailIdentity`; `ActivateEmailIdentity`; `SetUserActive`; `InsertChallenge`; `FindLiveChallenge`; `ConsumeChallenge`; `RecordChallengeFailure`; `CancelChallengeFamily`; `ChargeBucket`; `RecordBucketFailure`; `InsertDelivery`; `ClaimDelivery`; `MarkDeliverySent`; `MarkDeliveryTerminal`; `EraseDeliveryMaterial`; `CancelUnsentDeliveries`; `UpsertAuthRequest`; `FindAuthRequest`. `ConsumeChallenge` is a single conditional `UPDATE ... RETURNING` requiring a live, unconsumed, uncancelled, unexpired row with attempts below the limit and a matching proof digest; `RecordChallengeFailure` commits separately from the rolled-back business transaction, so a failed attempt is never refunded.

## Secrets, Hashing and Compromised Passwords

Add `AUTH_SECRET_KEY`: 32 bytes, standard base64, distinct from `AUTH_BROWSER_KEY`, with a `key_version` recorded on every row it protects. It encrypts delivery material with standard-library AES-256-GCM and keys the OTP proof digests, target digests and pseudonymous IP digests through domain-separated HMAC. **Both the API and the worker need this key** (the worker decrypts delivery material to send it); the worker still never receives `AUTH_BROWSER_KEY`. Startup fails with `identity_config_invalid` on a missing or malformed value, and no supplied value is ever logged.

Passwords use `golang.org/x/crypto/argon2.IDKey` with a fresh 16-byte salt and parameters encoded into the stored string. Start from the RFC 9106 memory-constrained profile, measure verification latency and concurrent memory use in the API container, then fix the parameters and record both the measurement and the chosen values in the convergence record. Validate the 15-to-128 code-point length before any hashing work. Never trim or truncate. Verification uses the library's constant-time comparison path.

The compromised-password check sits behind an interface with one HTTP implementation (Pwned Passwords range API, `Add-Padding: true`, five-character prefix only, bounded timeout, local suffix comparison) and one test implementation. Registration and password changes fail closed when the lookup is unavailable; login is unaffected. CI has no outbound network, so integration tests inject the test implementation and the HTTP implementation is covered by its own unit test against a local stub server. Never log or trace the password, its full hash, the prefix, the request URL or the response body.

## Budgets and Client Identity

Charge budgets inside the same transaction as the challenge or delivery they authorize, in the data model's lock order: account, challenge family, sorted buckets, delivery reservation. Working values come from plan.md and are not re-chosen here: email verification expires in fifteen minutes; five failures per purpose/target family; sixty-second resend cooldown with five sends per hour and ten per day per target; sign-in failures ten per account and fifty per client per fifteen minutes. A resend invalidates the prior proof, keeps the family attempt budget and cancels unsent deliveries for that family.

Client-keyed buckets need a trustworthy client address. Derive it only from the single configured `AUTH_TRUSTED_PROXY_HEADER` when the connection arrives from the configured proxy; otherwise use the direct remote address. When neither is configured, client-keyed buckets are skipped and account/target buckets still apply — a missing proxy configuration must not silently key every request to one shared bucket. Store only the HMAC digest of the address with the versioned secret key, never the address itself.

## Four HTTP Endpoints

All responses, including errors, are `no-store`. Operation IDs: `registerEmail`, `resendChallenge`, `verifyEmail`, `loginPassword`. `/auth/context` gains `enabled_methods: ["email_password"]`; its behavior is otherwise unchanged.

| Method/path | Result | Authentication and errors |
| --- | --- | --- |
| POST `/auth/register/email` | 202 `{challenge_id, next_allowed_at}` | Anonymous context + Origin/CSRF + Idempotency-Key. Identical shape and comparable timing for a new and an existing address. 400 invalid_request, 422 password_rejected, 429 rate_limited |
| POST `/auth/challenges/{id}/resend` | 202 `{challenge_id, next_allowed_at}` | Context bound to the challenge + Origin/CSRF + Idempotency-Key. No target override in the body. 404 not_found for a foreign challenge, 409 challenge_invalid, 429 rate_limited |
| POST `/auth/email/verify` | 200 `{result:"completed"}`; no session cookie | Context bound to the pending registration + Origin/CSRF. 409 challenge_invalid/challenge_expired/challenge_exhausted. Once-only: a repeat returns the same safe completed result and never re-authorizes |
| POST `/auth/login/password` | 200 `{result:"authenticated"}` and session/device cookies | Anonymous context + Origin/CSRF + Idempotency-Key. 401 invalid_credentials, 403 verification_required, 429 rate_limited |

Registration creates a pending `users` row (`access_state=pending_email`) with its `login_identities` row, one challenge family and one delivery, all in one transaction. A request for an address that already has an active identity creates nothing, charges the target bucket, and returns the same 202 shape; it can never replace existing password material. Verification atomically consumes the challenge, sets `verified_at` and flips the account to `active`; it does not create a session. Login verifies the password, then the access state: `pending_email` returns `verification_required`, which is safe because it is only reachable after a correct password. A non-existent address runs a verification against a fixed dummy hash before returning `invalid_credentials`. Only after both checks pass does login call `identity.Sessions.Issue` and set the session and device cookies with the attributes the session foundation already fixed.

MFA gating does not exist yet because no strong factor can be enrolled. Increment 3 (T063) inserts the pending-MFA branch between the password check and `Issue`; do not pre-build that branch here, and do not add session proof-metadata columns in this slice.

The `auth_requests` helper binds pre-authentication keys to the anonymous context plus purpose and target digest, and authenticated keys to user, operation and body HMAC. A reused key with a different body returns 409 idempotency_conflict. Only a result reference is stored, never a session token, code or password.

## Delivery

Issuance writes the delivery row and enqueues the job in the challenge transaction. The worker handler loads the delivery by ID, claims it (`pending` to `sending` conditional update), rechecks the challenge is still live, the account state and the kill switch, then sends outside any open transaction, then records the outcome and erases the encrypted material. A provider timeout or any ambiguous outcome sets `unknown` and stops: no automatic resend, because an ambiguous send may already have delivered. The user may request a separately budgeted resend. Exactly-once delivery is not promised, and the convergence record must say so.

Two transports: an in-process test transport, and a development transport that writes the rendered message to a mounted directory configured by `AUTH_EMAIL_DEV_DIR`. Messages never go to logs or traces. With no transport configured the method is unavailable and reports `method_unavailable` rather than silently dropping mail. No live provider adapter is written in this slice.

## Web Journey

`web/server/utils/authenticatedFetch.ts` builds a per-request fetch that forwards only `__Host-blob_session` to the fixed internal API base and nothing else; it must never be attached to the shared generated-client singleton, and the existing test forbidding cookies on the anonymous health path stays green. SSR responses carrying personalized data are `no-store`, and no challenge ID, code or token enters hydration.

Three pages — sign-up, sign-in and verify-email — cover every state: idle, submitting, generic failure, rate-limited with a visible countdown, challenge expired, resend sent, verification complete and verification required at sign-in. Every string lives in `web/i18n/locales/{ru,en,ar}.json`; no literal user-facing text in components. Arabic renders RTL. Each state is reachable by keyboard, errors are associated with their fields, and status changes are announced. Generic responses stay generic in the UI: the interface must not tell a visitor whether an address is registered.

`Caddyfile` and `compose.yaml` gain the trusted local HTTPS origin (`https://localhost:8443`) that the session foundation's configuration already names. Install Caddy's local trust root only through the documented explicit setup step in `README.md`. Production cookie rules are never weakened to make a test pass.

## Verification Required Before Completion

1. Real PostgreSQL throughout, real up migration. Registration creates exactly one pending user, identity, challenge and delivery, or nothing at all; a forced failure after the delivery insert leaves no partial state.
2. Duplicate registration against an active address returns the same shape and changes no password material. Timing of the existing-address and unknown-address paths is comparable in a recorded measurement.
3. Verification is once-only under concurrency: parallel consume attempts yield at most one activation, attempts are committed on failure and never refunded, and an expired or exhausted family cannot be revived by a resend.
4. Resend respects the cooldown, hourly and daily budgets, invalidates the prior proof and cancels unsent deliveries. Concurrent resends charge the bucket exactly once per permitted send.
5. Login: correct password on a pending account returns `verification_required` and issues no cookie; wrong password returns `invalid_credentials` with no account disclosure; a successful login issues exactly one session and reuses an existing owned device when its cookie is valid. Failure budgets apply per account and per client, and with no proxy configured the client bucket is skipped rather than shared.
6. Password boundaries: 14, 15, 128 and 129 code points; multi-byte and whitespace-only inputs are neither trimmed nor truncated; a compromised password is rejected; an unavailable lookup fails registration closed and leaves login working.
7. Worker fencing: a stale worker holding an old claim token cannot complete or fail a job reclaimed by another worker; the fenced attempt changes no row. An unregistered kind still fails safely. The delivery handler is idempotent across redelivery.
8. Ambiguous provider outcome marks the delivery `unknown`, erases its material, does not resend automatically and leaves the code invalid for use.
9. Idempotency: a replayed key with the same body returns the same reference; a different body returns 409; no response body stores a secret.
10. Secret canary over cookies, CSRF token, both keys, password, code, address and cursor passes the existing allowlist logging and tracing path. Every new response and error is `no-store`. Existing health, observability, session and process tests stay green.
11. Web: unit and SSR tests for the three pages in all three locales, plus a real-browser smoke over the trusted local HTTPS origin covering register, read the code from the development transport directory, verify, sign in and sign out, in Russian, English and Arabic.
12. Gates: Go tests with `TEST_DATABASE_URL` set, `go vet`, `go build`, migration up/down/up on a disposable database, generation run twice with no second-run drift, web typecheck and production build, `docker compose config --quiet` without a `.env`. Record the live email provider and public domain as still pending.

## Open Decisions Returned to Planning, Not Invented Here

If implementation finds that any of the following cannot hold, stop and return the contradiction to planning rather than weakening it: pending accounts are never usable before verification; registration is never an enumeration oracle; a job payload never carries a secret; an ambiguous send is never retried automatically; the compromised-password check never fails open for a new password. No development login shortcut, no test-only public endpoint and no relaxed cookie attribute is an acceptable workaround.
