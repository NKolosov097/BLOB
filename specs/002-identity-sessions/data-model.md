# Stage 1B Data Model

Status: Phase 1 design accepted for implementation on 2026-10-10. Working policy values are recorded in [plan.md](plan.md). This describes constraints and transitions, not migration code. UUIDs are application-created UUIDv7; times are UTC; all owner relationships use foreign keys.

## Records and Constraints

| Record | Required data | Invariants and indexes |
| --- | --- | --- |
| `users` | id, access_state, mfa_required, protection_state, security_version, created_at, updated_at | access_state: pending_email/active/restricted/deletion_pending; only Identity changes security fields. protection_state: never_enrolled/protected/recovery_required. security_version increments on credential/factor changes. No profile/name/username columns in 1B |
| `login_identities` | id, user_id, kind, comparison_key, issuer/subject for Google, verified_at, password_hash for email, created_at, revoked_at | Unique active (kind, comparison_key); Google uniqueness is canonical verified (issuer, subject). Email/phone are private. No provider tokens. Pending email reservations cannot overwrite an existing identity |
| `devices` | id, user_id, binding_hash, coarse browser/OS label, created_at, last_seen_at, revoked_at | Unique (user_id, binding_hash); no fingerprint, precise location or raw IP. A revocation is a session association revocation, not hardware blacklisting |
| `sessions` | id, user_id, device_id, token_hash, created_at, idle_expires_at, absolute_expires_at, revoked_at, first_factor_at, strong_at, strong_method_id, strong_purpose, proof_security_version | Unique token_hash; owner/active-expiry indexes. Full authenticated sessions only. Strong proof is valid only for the bound purpose, recency and current security_version |
| `passkeys` | id, user_id, credential_id, public_key, user_handle, sign_count, transports, backup_eligible/backed_up, label, created_at, last_used_at, revoked_at | Unique credential_id globally; immutable account user_handle; library validates credential and ceremony. Sync/counter handling follows validated credential semantics |
| `totp_credentials` | id, user_id, encrypted_secret, key_version, status, created_at, confirmed_at, last_accepted_step | One active TOTP per account; pending replacement cannot remove active credential. Atomic accepted-step update prevents replay |
| `recovery_codes` | id, user_id, set_id, code_hash, created_at, used_at, revoked_at | Random high-entropy codes; unique digest; atomic unused-to-used transition. New set invalidates old unused codes; never return stored digests |
| `auth_challenges` | id, family_id, browser_binding_hash, user_id nullable, purpose, target_key_hash, proof_digest/encrypted_protocol_state, created_at, expires_at, consumed_at, cancelled_at, first_factor_method_id/verified_at, security_version, recovery_state | Purpose-specific allowlisted state, no generic executable payload. Owner/target/purpose/expiry indexes. Protocol state is bounded and encrypted if secret. This record also carries short-lived MFA-pending, linking, step-up and restricted recovery context |
| `auth_buckets` | scope, keyed_subject_digest, purpose, window_start, failures, sends, next_send_at, expires_at | Unique bucket key; transactionally enforce account/target/browser/IP budgets. Resend/recreated challenges use the same relevant bucket. Separate short-lived key material for pseudonymous IP keys |
| `auth_deliveries` | id, challenge_id nullable, user_id nullable, channel, purpose, encrypted_destination/body_material, key_version, status, provider_key, reservation_amount/currency, created_at, expires_at, sent_at | Unique provider_key; statuses pending/sending/sent/failed/unknown/cancelled. Jobs contain only delivery ID. Cost reserved before external effect. Secret material erased at expiry/consumption and terminal processing |
| `auth_requests` | subject_digest, operation, idempotency_key_digest, body_hmac, result_reference/status, created_at, expires_at | Unique subject/operation/key. Response stores no raw session, code, TOTP secret or password. A reused key with different body returns conflict |
| existing `jobs` extension | claim_token, plus existing lease/attempt/status data | Claim renews token; complete/fail require matching live claim. Handler registry supplies known job kinds. Handler execution bounded below lease duration |

Use existing tables where they already satisfy these purposes; no new generic repository, event framework or polymorphic business-object model. Separate auth state from public profile data.

## Identity Keys and Enrollment

Email comparison normalizes the domain only, preserves the local part and never rewrites provider aliases. Enforce a reviewed ASCII email subset initially; internationalized email support is a separately reviewable requirement, not silently approximated. Phone input must be explicit international format; normalize/validate through the selected phone library, then apply configured allowed regions. No guessing country from IP. Google metadata email does not become a password/recovery channel until separately verified by Blob.

Email registration creates a pending account/identity and one verification family. Requests for an existing address produce the same public response but cannot replace password material. Verification atomically activates the pending identity/account; it does not create a full session. A later explicit login does. Google/phone sign-in may create an account only after provider/OTP ownership proof. Linking always targets the already authenticated account and never merges two existing owners.

A working sign-in method is a verified email/password identity, verified Google/phone identity or active passkey. TOTP/recovery codes alone are not first-factor sign-in methods. All link/unlink operations lock the account before counting methods. Unique constraints resolve races with other accounts.

Strong-factor removal: a protected account keeps at least one active TOTP/passkey; replacement proves the new factor before removing the old. Disabling ordinary-login MFA does not disable strong verification for security changes. Removing every strong factor would make later required security changes impossible and is therefore rejected by the proposed policy. This working rule was accepted with the planning package on 2026-10-10, rather than being attributed to the original architecture.

## Authentication and Session Transitions

1. GET context issues a signed browser-binding cookie and matching CSRF token without a database mutation. A POST starts a challenge bound to that browser and purpose.
2. Successful first factor produces either a full session or a short-lived MFA-pending challenge. A pending challenge cannot call ordinary authenticated endpoints.
3. Successful TOTP/passkey completes required MFA; invalidate the pending context, rotate any prior session and issue a fresh opaque session cookie. Validated passkey login can satisfy the strong authentication requirement directly.
4. First-factor reauthentication records time/method for the requested purpose. Step-up with TOTP/passkey records current-version, purpose-bound strong proof and rotates the session. Neither extends the original absolute expiry during a session rotation.
5. A request authenticates through the current active session/account state. Update passive last-seen/idle accounting without creating, linking, consuming or elevating authentication through GET. Require idle deadline and absolute deadline to both remain valid.
6. Logout revokes current session; device revoke transactionally revokes all its sessions; all-session revoke includes current. Credential recovery revokes all old sessions. Never cache revocation across requests without a validated invalidation mechanism.

Use random 256-bit session and browser-binding secrets. Store session token hashes only; return token only as an HttpOnly cookie. Store low-entropy OTP proofs as keyed digests, not unsalted enumerable hashes. Use separate configured keys for OTP digests, encrypted secrets/delivery material and browser-binding/CSRF signing; keys have version identifiers and documented rotation. Reusable plaintext secrets exist only while displaying setup or performing a bounded provider operation.

## First Strong Factor and Recovery

Bootstrap is allowed only when protection_state=never_enrolled and recent proof of an existing verified method is bound to the same account/session. Confirming the new TOTP/passkey atomically sets protected. Deleting records, loss reports, disabling ordinary MFA or failed enrollment do not reset that flag.

Recovery-code authority is deliberately narrow:

1. Prove an existing first-factor method freshly. For password loss, verify the already bound email channel as recovery proof; do not grant full access or change MFA yet. Google/phone proof is similarly account-bound.
2. Present one unused recovery code in that same browser-bound recovery challenge. One code without first-factor proof is always rejected. Consume it atomically and enter `replacement_required`.
3. This context permits only replacement TOTP/passkey enrollment, confirmation or cancellation; it cannot export, link identities, change unrelated security settings or call ordinary account endpoints.
4. Confirm a replacement strong factor. Atomically commit any staged password reset, retire explicitly replaced factors, rotate recovery codes, cancel competing recovery contexts, increment security_version, revoke all sessions and enqueue old-channel alerts. Return to sign-in rather than silently logging in.
5. If all strong factors and codes are gone, no recovery transition exists. If the code is consumed but setup is abandoned, that code stays consumed; another remaining factor/code is needed. Warn before consumption and require explicit acknowledgement.

A lost first factor plus no alternative first-factor/recovery-channel proof is also insufficient even if a recovery code remains. Explain this in backup guidance. These details implement the accepted conservative policy; they do not broaden recovery-code authority.

## Atomic Operations and Delivery

Use a stable lock order: account, challenge/family, sorted abuse buckets, delivery reservation. Failed attempts are committed even when authentication fails; business transactions are rolled back without resetting already recorded abuse decisions. Network requests occur outside database locks. Provider callback verification claims a challenge once; a crash/ambiguous exchange invalidates that ceremony and requires a new start rather than replaying its authorization code.

Challenge creation/resend, budget charge, delivery record and job enqueue commit together. A resend invalidates the prior proof, keeps its family attempt budget and cancels unsent old deliveries. Before sending, lock/claim the delivery and recheck challenge validity, account state, kill switch and cost reservation. Never send a new code for an old expired family. External effects may already have happened during a race; such a code remains invalid and cannot authorize access.

Authenticated creates bind idempotency to user/operation/body HMAC; pre-authentication creates bind it to browser context plus purpose/target digest. Store references rather than secret response bodies. Consuming endpoints use atomic challenge identity as replay protection: a duplicate completed submission returns a safe completed/conflict result, never reproduces a session token or one-time secret. Lost secret responses require restarting the relevant flow or regenerating codes through fresh strong verification.

## Retention, Revocation and Handoff

Apply the plan's working retention intervals through bounded cleanup jobs. Retain no OTP, OIDC verifier, TOTP pending secret or delivery ciphertext after its useful lifetime. Never purge an active factor to satisfy generic challenge cleanup. Do not expose inactive device/session records beyond the owner.

Stage 1F owns account deletion/restriction workflows. Identity exposes account-state checks, strong-verification requirements and account-wide revocation. Stage 1G restore procedure must invalidate all restored sessions/challenges/recovery contexts before reopening traffic and reconcile later credential revocations; a backup must not restore a revoked factor's authority. The choice of protected revocation journal storage belongs to that restore plan. No restored session is trusted merely because its saved expiry is still in the future.

## Verification Traceability

FR-001/002: identity ownership and activation. FR-003/006/007: method/factor constraints and bootstrap. FR-004/005: password/challenge storage and budgets. FR-008: restricted recovery. FR-009/010: sessions/devices. FR-011/014: browser binding and private representations. FR-012/015: buckets, reservations and delivery. FR-013: current security version/state. FR-016: safe localized UI representations are defined in the [contract](contracts/identity.md) and [quickstart](quickstart.md).