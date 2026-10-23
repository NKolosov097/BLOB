# Identity HTTP and UI Contract

Status: Phase 1 contract accepted for implementation on 2026-10-10. Transcribe each increment into the single authoritative `api/openapi.yaml`; this document is not a second type-generation source. All API paths below are relative to `/api/v1`. Working defaults are recorded in [plan.md](../plan.md).

## Shared Rules

- JSON requests/responses except the web callback page. Strict allowlisted request fields; server chooses user IDs, privilege, timestamps and proof state. Body cap: 16 KiB normally, 64 KiB for WebAuthn material; validate decoded dimensions before expensive work.
- Every response, including errors and context, is `Cache-Control: no-store`. No session tokens, password digests, stored secrets, provider tokens or raw recovery codes in ordinary representations.
- All POST/PATCH/DELETE requests require a matching CSRF token and exact trusted Origin. Missing Origin is rejected. CORS permits exact configured first-party origins only. GET has no account/authentication mutation; context GET may issue the transport-only browser binding cookie.
- `U`: signed anonymous browser context. `P`: browser-bound challenge with fresh existing first-factor proof. `A`: full active owner session. `S`: A plus recent purpose-bound TOTP/passkey proof at current security_version. `E`: S, or A with fresh existing-method proof only when never enrolled a strong factor. `R`: restricted recovery context after P plus one recovery code. Auth level does not waive current ownership/account-state checks.
- `C`: Idempotency-Key required for a create/start; bind owner or anonymous browser context, operation and body HMAC. Same key/different body: 409. Never persist secret response bodies. `O`: once-only challenge consume; repeats cannot reauthorize or return secret material. `I`: naturally idempotent; repeat ends in the same state. `V`: edit uses If-Match/security version and rejects stale state. `-`: read only.
- Errors retain `{error:{code,message,request_id,fields?}}`. Common outcomes: 400 invalid_request, 401 authentication_required/invalid_credentials, 403 csrf_failed/origin_denied/step_up_required, 404 not_found for missing or nonowned objects, 409 state_conflict/idempotency_conflict, 412 version_conflict, 413 request_too_large, 429 rate_limited with Retry-After, 503 method_unavailable. Challenge errors use safe challenge_invalid/challenge_expired/challenge_exhausted; do not reveal whether an unknown account exists.
- Identity-create conflicts may be reported only after proof and authorization make disclosure appropriate. Registration/reset initiation returns the same 202 shape for existing and unknown addresses. Timings and provider errors must not become an account-enumeration oracle.

## Endpoints

`CSRF=Y` also means exact Origin. All mutations return only safe summaries or the explicitly identified one-time setup result.

| Method | Path | Auth | CSRF | Input -> result | Specific errors | Retry |
| --- | --- | --- | --- | --- | --- | --- |
| GET | `/auth/context` | U or A | - | No input -> CSRF token, enabled methods, public policy metadata; transport binding cookie if absent | method_unavailable | - |
| POST | `/auth/register/email` | U | Y | email,password,locale -> generic challenge acknowledgement; pending account only | password_rejected,rate_limited | C |
| POST | `/auth/challenges/{id}/resend` | U/P/A bound to challenge | Y | no target override -> generic acknowledgement, next_allowed_at | challenge_invalid,rate_limited,method_unavailable | C |
| POST | `/auth/email/verify` | U bound to pending registration | Y | challenge_id,code -> verified status; no login cookie | challenge_invalid/expired/exhausted | O |
| POST | `/auth/login/password` | U | Y | email,password -> full session cookie or mfa_pending challenge | invalid_credentials,verification_required,rate_limited | C |
| POST | `/auth/phone/start` | U | Y | phone, intent=register_or_login -> generic challenge acknowledgement | invalid_request,rate_limited,method_unavailable | C |
| POST | `/auth/phone/complete` | U bound to challenge | Y | challenge_id,code -> full session or mfa_pending; may create verified phone-only account | challenge_invalid/expired/exhausted | O |
| POST | `/auth/google/start` | U | Y | purpose=login -> authorization_url and bound transaction | method_unavailable,rate_limited | C |
| POST | `/auth/google/complete` | U/P/A bound to transaction | Y | transaction_id,code,state -> result appropriate to stored login/link/reauth purpose | challenge_invalid,provider_rejected,identity_conflict | O |
| POST | `/auth/passkeys/options` | U | Y | purpose=login -> WebAuthn request options, challenge_id | method_unavailable,rate_limited | C |
| POST | `/auth/passkeys/complete` | U bound to challenge | Y | challenge_id,assertion -> full session after required verification | challenge_invalid,invalid_credentials | O |
| POST | `/auth/mfa/totp` | P | Y | challenge_id,totp -> full session; rotate prior identifier | invalid_credentials,challenge_exhausted | O |
| POST | `/auth/mfa/passkeys/options` | P | Y | challenge_id -> account-bound request options | challenge_invalid | C |
| POST | `/auth/mfa/passkeys/complete` | P | Y | challenge_id,assertion -> full session | challenge_invalid,invalid_credentials | O |
| POST | `/auth/reauth/start` | A | Y | purpose,method_id -> method-specific challenge or Google URL | invalid_request,method_unavailable | C |
| POST | `/auth/reauth/complete` | A, or P strictly for an existing strong factor in recovery purpose | Y | challenge_id,method-specific proof -> recent first-factor or strong proof; rotate on strong proof | invalid_credentials,challenge_invalid | O |
| POST | `/auth/logout` | A or already cleared context | Y | no input -> revoke current session and clear cookie | csrf_failed | I |
| GET | `/identity/me` | A | - | -> id,account status,mfa_required,security_version | authentication_required | - |
| GET | `/identity/security` | A | - | -> masked methods, factor labels, recovery-code count, proof requirements, ETag | authentication_required | - |
| POST | `/identity/methods/start` | S | Y | kind=email/phone/google plus new target/password where applicable -> link challenge/options | identity_conflict,password_rejected,method_unavailable | C |
| POST | `/identity/methods/complete` | S with matching challenge | Y | challenge_id,proof -> verified linked method; Google uses its complete endpoint | identity_conflict,challenge_invalid | O |
| DELETE | `/identity/methods/{id}` | S | Y | If-Match -> method removed, current session rotated | last_method,state_conflict,version_conflict | I+V |
| PATCH | `/identity/password` | S | Y | new_password,If-Match -> replace existing password, revoke other sessions, rotate current | password_rejected,version_conflict | V |
| PATCH | `/identity/mfa` | S | Y | required boolean,If-Match -> ordinary-login MFA setting; security changes still need strong proof | last_strong_factor,version_conflict | V |
| POST | `/identity/totp/start` | E or R | Y | replacement_id optional -> challenge_id, one-time setup URI/secret | step_up_required,state_conflict | C |
| POST | `/identity/totp/complete` | E or R bound to setup | Y | challenge_id,totp -> active factor under E, staged replacement under R; first enrollment includes new recovery codes | invalid_credentials,challenge_invalid | O |
| DELETE | `/identity/totp/{id}` | S | Y | If-Match -> remove factor only if another strong factor remains | last_strong_factor,version_conflict | I+V |
| POST | `/identity/passkeys/options` | E or R | Y | label,replacement_id optional -> creation options,challenge_id | step_up_required,state_conflict | C |
| POST | `/identity/passkeys/complete` | E or R bound to setup | Y | challenge_id,attestation -> enrolled factor under E, staged replacement under R; first enrollment includes new recovery codes | credential_conflict,challenge_invalid | O |
| DELETE | `/identity/passkeys/{id}` | S | Y | If-Match -> remove credential | last_method,last_strong_factor,version_conflict | I+V |
| POST | `/identity/recovery-codes` | S | Y | explicit replace acknowledgement -> one-time new code set; old codes invalidated | state_conflict | C |
| POST | `/auth/recovery/start` | U | Y | purpose=password_reset/factor_loss, existing method identifier -> generic proof challenge | rate_limited,method_unavailable | C |
| POST | `/auth/recovery/first-factor` | U bound to recovery challenge | Y | challenge_id,existing-method proof -> restricted P; provider challenge routing below | challenge_invalid,invalid_credentials | O |
| POST | `/auth/recovery/code` | P in recovery purpose | Y | challenge_id,recovery_code,loss acknowledgement -> R, never full login or strong step-up | invalid_credentials,challenge_exhausted | O |
| POST | `/auth/recovery/password` | P for unprotected account; P+strong proof or R for protected account | Y | challenge_id,new_password -> unprotected reset completes; protected reset is staged until required proof/replacement | password_rejected,step_up_required | O |
| POST | `/auth/recovery/complete` | proven replacement in R or P+existing strong factor | Y | challenge_id,confirmation -> apply recovery, rotate codes, revoke sessions, queue alerts, require fresh sign-in | recovery_incomplete,state_conflict | O |
| GET | `/identity/sessions` | A | - | -> stable list, current flag, coarse device labels and times | authentication_required | - |
| GET | `/identity/devices` | A | - | -> stable list of owned device associations | authentication_required | - |
| DELETE | `/identity/sessions/{id}` | A | Y | -> revoke owned session, clear cookie if current | not_found | I |
| DELETE | `/identity/devices/{id}/sessions` | A | Y | -> revoke all sessions for owned device | not_found | I |
| DELETE | `/identity/sessions` | A | Y | explicit all confirmation -> revoke all including current, clear cookie | authentication_required | I |

Revoking sessions is an access-reducing action and uses A without step-up under the accepted baseline; changing credentials/factors uses S. This distinction was accepted with the planning package on 2026-10-10.

## Payload and Workflow Details

**Authentication result**: one of authenticated, verification_required, mfa_required, recovery_required, completed. Only authenticated may set a full session cookie. Other results carry a bounded challenge ID and next allowed action names; IDs alone are not bearer authorization. Every challenge is checked against its signed browser binding, purpose, owner and lifetime.

**Method proof union**: password; email/phone code with challenge ID; WebAuthn assertion/attestation; Google transaction result. Purpose and method are chosen at start and cannot be switched in complete. Reauth can return request options for a passkey or send a phone code. Google reauth uses `/auth/google/complete` against the stored reauth transaction; its result updates only that account's proof. Recovery start similarly returns a Google URL or a delivery/password challenge; Google completion advances its stored recovery purpose, never ordinary login. Password-reset email proof is bound to the existing account, not a new arbitrary address.

**Strong recovery with a remaining factor**: recovery first-factor may issue TOTP/WebAuthn proof options for the existing factor; the same `/auth/reauth/complete` contract permits a P recovery context only for that factor verification (no unrelated session elevation). Record the result on the recovery challenge; `/auth/recovery/complete` applies the staged change. The ordinary A-only reauth path remains separate in authorization checks.

**Recovery-code authority**: `/auth/recovery/code` demands recent first-factor proof plus the code. Code-only attempts are rejected. R can use only replacement-factor endpoints and recovery completion. First-factor + recovery code does not authorize method linking, exports, arbitrary password changes or ordinary access. Any password reset must have been staged within the bound password-reset purpose. Completing TOTP/passkey under R proves and stages replacement; `/auth/recovery/complete` is the only final commit, so abandoned replacement cannot partly remove protection.

**One-time output**: TOTP provisioning data and raw recovery codes are returned only from explicit setup/regeneration responses, never in SSR hydration or GET security state. Do not cache, log or persist their response body. If the response is lost, the user restarts pending TOTP setup or regenerates codes with valid proof; idempotency replays return a safe operation reference, not secrets.

**Lists/versions**: session/device lists order by created_at then UUID; no unbounded list. Maximum 100 active device associations per account, reject new creation with an actionable limit message until one is revoked; session list page size 50 with opaque signed cursor. Security edits use ETag derived from security_version. Creates/consumes validate the version captured when authorization started; a concurrent security change can force reauthentication.

## Google Callback Page

The web route `/auth/google/callback` is GET only and has no authentication side effects. It renders a first-party confirmation page with a clean error/cancel path. It does not exchange code, consume state, create/link an account, or set a full session. The page obtains context, keeps code/state in local memory only, removes query parameters with history replacement, and on user confirmation calls the POST completion endpoint with CSRF and exact Origin. Never interpolate secrets into scripts or shared hydration. On cancellation clear local data; server transaction simply expires.

Use `Cache-Control: no-store`, `Referrer-Policy: no-referrer`, a nonce/hash CSP without third-party script/network dependencies, and access logging limited to route templates. Do not echo provider error_description. Only configured same-origin return paths are accepted; never redirect to user-supplied absolute URLs. Reload/back/replay cannot complete an already consumed transaction.

## UI Surfaces and Acceptance Mapping

- Sign-up/sign-in: email verification, Google, phone OTP, passkey choice, safe resend countdown and generic failures (FR-001/002/004/005).
- MFA/reauthentication: method choice from enrolled factors, accessible prompt, expired-proof recovery and explicit first-factor bootstrap warning (FR-003/006/007).
- Recovery: explain first-factor-plus-code, loss acknowledgement, backup guidance and irreversible total loss; no implied support override (FR-008).
- Security settings: masked methods, factor management, recovery-code generation and purpose-specific reauthentication (FR-003/006/007/013).
- Sessions/devices: current flag, coarse metadata, individual/all revocation confirmation (FR-009/010).
- Every surface: Russian/English/Arabic RTL, labels, focus, keyboard, status announcements, no secret hydration/cache/telemetry, bounded attempts and delivery failures (FR-011/012/014/015/016).