# Feature Specification: Stage 1B Identity and Sessions

**Feature Branch**: `002-identity-sessions`

**Created**: 2026-10-09

**Status**: Accepted as the working specification for implementation on 2026-10-10 after review and user continuation. External providers and public domains remain deployment inputs.

**Input**: Deliver the complete Stage 1B scope from the [MVP architecture](../../docs/specs/2026-09-14-blob-mvp-design.md) and [Stage 1A handoff](../../docs/plans/2026-09-14-blob-stage-1a-platform-foundation.md): registration, sign-in, Google, phone OTP, 2FA, passkeys, recovery, sessions and devices.

## User Scenarios & Testing

All stories are required for Stage 1B. Priority defines sequencing, not optional scope.

### User Story 1 - Create and access an account (Priority: P1)

A visitor creates an account, verifies ownership and signs in using email/password, Google or phone OTP. An existing owner can also sign in with an enrolled passkey.

**Why this priority**: Every later social feature needs a verified account.

**Independent Test**: Complete each supported entry path with its verification prerequisites and sign out.

**Acceptance Scenarios**:

1. **Given** a new email/password registration, **When** email is unverified, **Then** full account access is unavailable; successful verification enables normal sign-in.
2. **Given** a valid Google or phone identity, **When** its required verification completes, **Then** its owner can access the corresponding account, including a phone-only account.
3. **Given** invalid credentials or an expired, used or exhausted challenge, **When** sign-in is attempted, **Then** no authenticated access is granted.
4. **Given** an account requiring MFA, **When** only its first authentication step succeeds, **Then** access remains limited until its MFA requirement is satisfied.

### User Story 2 - Control sessions and devices (Priority: P1)

An owner sees where they are signed in and can end access on this browser, a selected device or every device.

**Why this priority**: Lost devices and stolen sessions must not retain access.

**Independent Test**: Sign in through two browser sessions, revoke one and then revoke all.

**Acceptance Scenarios**:

1. **Given** two active devices, **When** one is revoked, **Then** its affected sessions cannot authorize subsequent requests and the other device remains usable.
2. **Given** active sessions, **When** all are revoked, **Then** every affected session loses access according to the reviewed all-session policy.
3. **Given** an expired session or another user's device identifier, **When** access or revocation is attempted, **Then** the request is denied without exposing private records.

### User Story 3 - Add strong authentication and manage methods (Priority: P1)

An owner enrolls TOTP or passkeys, saves recovery codes and safely manages linked methods.

**Why this priority**: Sensitive operations require stronger proof than password or SMS alone.

**Independent Test**: Enroll a first strong factor, complete step-up verification, link another method and attempt removal of the last working method.

**Acceptance Scenarios**:

1. **Given** no existing strong factor, **When** the owner freshly authenticates with an existing verified method, **Then** they can enroll and prove their first TOTP or passkey.
2. **Given** an existing strong factor, **When** a security setting or linked identity changes, **Then** recent TOTP/passkey verification is required; the new method also proves ownership.
3. **Given** a Google identity with the same email as an existing account, **When** it signs in, **Then** email matching does not merge accounts or grant access to that existing account.
4. **Given** one remaining working method, **When** it is removed, including concurrent removals, **Then** removal is rejected.

### User Story 4 - Recover without bypassing protection (Priority: P1)

An owner recovers a lost credential using an approved remaining proof, while existing stronger protection remains effective.

**Why this priority**: Recovery must not become an easier account-takeover path.

**Independent Test**: Recover with a valid remaining proof, retry it, and exercise loss of every strong factor and recovery code.

**Acceptance Scenarios**:

1. **Given** valid recovery proof, **When** recovery completes, **Then** affected sessions are revoked, the old channel receives a security alert and the account's MFA policy is preserved.
2. **Given** enrolled stronger protection, **When** only SMS or email/password is available, **Then** recovery cannot silently remove that protection.
3. **Given** all passkeys, TOTP access and recovery codes are lost, **When** recovery is requested, **Then** the service explains that access cannot be recovered; there is no support override.
4. **Given** enrollment of strong protection, **When** the owner completes setup, **Then** the interface clearly warns of irreversible loss and encourages backup methods and saved recovery codes.

### User Story 5 - Complete flows accessibly (Priority: P2)

A user completes account-security journeys in Russian, English or Arabic with keyboard and assistive technology.

**Why this priority**: Authentication must not prevent access because of language or input method.

**Independent Test**: Complete the critical journey in each locale, including Arabic RTL, and inspect focus and status announcements.

**Acceptance Scenarios**:

1. **Given** a supported browser and locale, **When** a challenge succeeds, fails or expires, **Then** instructions, focus and announcements make the next action clear.
2. **Given** an unavailable or cancelled authenticator, **When** passkey authentication ends, **Then** the user can choose another enrolled method without weakening account protection.

### Edge Cases

- Concurrent identity linking, method removal, OTP consumption and recovery-code use have one consistent outcome.
- Resending a challenge never restores exhausted attempts; delayed delivery does not resurrect expired proof.
- Changing a credential or session state during an in-flight request cannot authorize a later request using stale state.
- Delivery outages, repeated jobs and spending exhaustion do not produce unbounded sends.
- Session rotation invalidates the previous identifier; activity cannot extend absolute expiry.
- Two users and an anonymous visitor never receive each other's account data through shared page state or caches.
- Untrusted origins, tampered provider responses, wrong-purpose challenges and forged ownership are rejected.
- Missing strong factors are not confused with never having enrolled one; removing/replacing a factor cannot reopen the bootstrap exception.

## Requirements

### Functional Requirements

| ID | Requirement and acceptance condition |
| --- | --- |
| FR-001 | Support all sign-in methods in Story 1 and multiple verified methods on one account. Email/password registration cannot obtain full access before email verification. Phone-only accounts are allowed. |
| FR-002 | Google ownership is determined by its verified issuer and subject, never email matching. Reject replay, invalid correlation and conflicting ownership, including concurrent linking. |
| FR-003 | Linking/unlinking requires recent existing-method reauthentication; linking proves the new method. Linking identities and security changes require TOTP/passkey except the confirmed first-strong-factor bootstrap. Never remove the last working method. |
| FR-004 | Support passwords of at least 15 characters for password-only access and a maximum of at least 64 characters. Reject known compromised passwords; never retain or expose plaintext passwords. |
| FR-005 | Verification codes are purpose-bound, expiring and attempt-limited; one successful use consumes the proof atomically. Resend does not reset attempts. |
| FR-006 | Support TOTP enrollment, confirmation, authentication and replacement/removal under FR-003. Protect its secret; recovery codes are protected and single-use. |
| FR-007 | Support passkey enrollment, authentication and removal under FR-003. Reject invalid, expired, replayed and wrong-account challenges. |
| FR-008 | Recovery uses short-lived single-use proof, preserves stronger protection, alerts the old channel and revokes sessions. Loss of all strong factors and recovery codes is unrecoverable; warn beforehand and encourage backups. |
| FR-009 | Sessions are opaque, private and revocable. Rotate identifiers after login, MFA and privilege elevation. Enforce idle and absolute expiry; expired/revoked/previous identifiers cannot authorize new requests. |
| FR-010 | Owners can view their sessions/devices, sign out, revoke one device or all sessions. Another user cannot list or change these records. |
| FR-011 | State-changing browser requests require protection against cross-site request forgery and untrusted origins. Reading a resource must not change account/security state. |
| FR-012 | Bound authentication attempts, message resends and SMS spending. A delivery failure or replay must not bypass protection or create unlimited charges. Support disabling SMS operationally. |
| FR-013 | Authorize Identity operations using current ownership and account state, not client-supplied privilege. Provide recent strong verification and session revocation for later lifecycle/moderation features. |
| FR-014 | Private account responses and personalized pages cannot enter shared caches. Errors are stable and safe. Telemetry excludes credentials, tokens, email/phone, cookies and other sensitive request data. |
| FR-015 | Required email/SMS delivery survives transient failures with bounded retries, rechecks current challenge/account state and does not treat repeated delivery as new authorization. Email is for verification, recovery and security, not social notifications. |
| FR-016 | Critical journeys work in Russian, English and Arabic/RTL, current Chrome/Edge/Firefox/Safari, system light/dark theme, keyboard and screen-reader basics. |

### Key Entities

- **Account**: stable owner identity, verification and account-access state.
- **Sign-in method**: a verified credential or external identity uniquely linked to an account.
- **Challenge**: purpose, owner/target, lifetime, attempts and consumed state.
- **Strong factor and recovery code**: proof supporting MFA, sensitive actions or approved recovery.
- **Session and device**: authenticated access, expiry, revocation and owner-visible device association.
- **Delivery/security event**: required external effect, outcome and minimal operational evidence.

## Success Criteria

### Measurable Outcomes

- **SC-001**: All five stories pass their acceptance scenarios; all four sign-in methods are demonstrated.
- **SC-002**: Every tested wrong-owner, replay, expired-proof, MFA-bypass and concurrent-consumption case is rejected without granting unintended access.
- **SC-003**: After revocation or rotation completes, zero subsequent requests using the affected old session are accepted.
- **SC-004**: Three locale journeys pass accessibility/RTL checks; every supported browser completes its applicable authentication journeys.
- **SC-005**: Cross-user/anonymous isolation and synthetic-secret checks find zero leaked account records or authentication secrets.
- **SC-006**: Configured attempt and spending caps remain effective during resend, concurrency and delivery-retry tests.

## Assumptions

### Confirmed decisions (2026-10-09)

1. Losing all passkeys, TOTP access and recovery codes is unrecoverable; warn and encourage backups.
2. Email/password accounts receive full access only after email verification.
3. First TOTP/passkey enrollment permits fresh reauthentication with an existing verified method; later security changes require TOTP/passkey.

### Working defaults accepted for implementation (2026-10-10)

Ordinary-user MFA is optional. Passkeys are enrolled after creating an account through another method. Name/username/profile setup belongs to 1C. Verification and recovery initiation use generic responses to reduce account enumeration. Device revocation ends associated sessions but does not permanently blacklist a physical device. These defaults were presented in the planning package and accepted as the implementation baseline through the user continuation on 2026-10-10; they are not retroactively attributed to the original architecture. Detailed policies and numerical values are grouped in [plan.md](plan.md).

### Scope and dependencies

The entire named 1B scope remains included. Profiles/social graph are 1C; content is 1D; feed/search is 1E; moderation and account export/deletion are 1F; full release/WAF/restore/load hardening is 1G. Cryptographic messaging-device enrollment and history recovery are Stage 3. No age collection, social notifications, extra identity providers or account merging are added.

Stage 1B relies on accepted Stage 1A interfaces and verification. Email/SMS providers and public authentication domains remain deployment dependencies; no provider purchase or spending is authorized by this document. Architecture security requirements apply and are mapped into the technical plan rather than duplicated as implementation details here.