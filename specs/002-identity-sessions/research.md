# Stage 1B Research and Design Decisions

Date: 2026-10-09. Status: technical design accepted as the implementation baseline on 2026-10-10. Product values are recorded in [plan.md](plan.md). Sources below were read on this date; they support technical choices, not claims that Blob is implemented or compliant.

## R1. Reuse the existing boundaries

Decision: extend the Go Identity module and existing OpenAPI/pgx/sqlc/worker paths. Consumers depend on narrow identity/session functions; no generic auth framework. Existing code has health only, GET-only web access and no public delivery-handler registration. These are implementation seams, not reasons to add another service.

Alternative rejected: external all-in-one auth service would replace the agreed learning architecture and create provider-specific policy. A custom protocol implementation is also unnecessary.

## R2. OIDC and the read-only callback

Decision: use `github.com/coreos/go-oidc/v3/oidc` for discovery and token verification with `golang.org/x/oauth2` for code exchange and S256 PKCE. Store a single-use, browser-bound transaction before redirect. Verify issuer, audience, expiry, signature, nonce and returned state. Treat Google issuer aliases only through the explicitly configured verifier; normalize to the configured canonical issuer after validation. Never derive ownership from email. These responsibilities follow [Google's server flow](https://developers.google.com/identity/openid-connect/openid-connect), [OIDC Core](https://openid.net/specs/openid-connect-core-1_0.html), the [go-oidc project](https://github.com/coreos/go-oidc) and [Go OAuth2 API](https://pkg.go.dev/golang.org/x/oauth2).

Application design inference: the registered callback is a GET page that renders a confirmation control without exchange, transaction consumption or session creation. First-party script removes query data from the address bar and submits code/state in a same-origin JSON POST after confirmation. Use no-store, no-referrer and a strict CSP; no third-party assets or analytics. Do not serialize callback data into general Nuxt hydration, traces or server access logs. A reload after URL cleanup restarts sign-in. Google documents the query response; the additional confirmation step is Blob's design, not a provider requirement.

Only request `openid email`; do not retain provider access/refresh tokens or request offline access. Linking binds the transaction to the current account and purpose. Login never overwrites an existing account session with a different account without an explicit sign-out/restart. Provider errors are mapped to safe codes.

## R3. Passkeys

Decision: use `github.com/go-webauthn/webauthn` with browser-native credentials APIs. Verify library-provided registration/assertion results against the stored ceremony, configured RP ID/origins and account binding. Require user verification. Store credential public keys, user handle, transports, backup flags and sign counter; a counter anomaly is evaluated using credential/backup semantics rather than banning every synced credential. This follows the [WebAuthn specification](https://www.w3.org/TR/webauthn-3/) and [library documentation](https://github.com/go-webauthn/webauthn).

Working baseline: discoverable credentials, no attestation-based device restriction and no additional browser SDK. Enrollment follows account creation. A verified passkey can complete login and strong step-up; its use never exposes a private key to the server. Alternative rejected: implementing WebAuthn verification manually.

## R4. Passwords, TOTP and recovery

Decision: use `golang.org/x/crypto/argon2.IDKey`, fresh random salts, encoded parameters and constant-time comparison. Start the benchmark with RFC 9106's memory-constrained profile, then measure container memory, concurrent verification and latency before selecting production parameters. No performance number is presumed. See [Go Argon2](https://pkg.go.dev/golang.org/x/crypto/argon2) and [RFC 9106](https://www.rfc-editor.org/rfc/rfc9106.html).

Decision: use `github.com/pquerna/otp/totp`, confirm enrollment before activation and atomically reject already-accepted time steps. Encrypt TOTP secrets using standard-library authenticated encryption with externally configured, versioned keys. Sources: [RFC 6238](https://www.rfc-editor.org/info/rfc6238/) and [the TOTP library](https://github.com/pquerna/otp).

Selected compromised-password source: the Pwned Passwords range API. Hash the complete password locally, transmit only the first five hash characters with Add-Padding enabled, compare suffixes locally and discard the response after checking. Never log the password, full hash, prefix, response body or request URL. Use a bounded timeout and fail closed for registration/password changes when lookup is unavailable; existing login and other methods remain usable. This discloses a hash-prefix query to the provider, not the full password; it is the recorded privacy tradeoff. The official [Pwned Passwords documentation](https://haveibeenpwned.com/API/v3#PwnedPasswords) describes the range and padding mechanisms. Alternative rejected: operating a full downloaded corpus and refresh pipeline for the initial one-developer service.

Recovery is a product policy, not inherited from a library: fresh proof of an existing first-factor method PLUS a recovery code yields only a restricted factor-replacement context. A recovery code alone grants neither login nor strong step-up. Consuming a code and creating that restricted context is atomic. Prove a replacement strong factor, rotate codes and revoke prior sessions before ordinary access. If every strong factor and code is lost, there is no support override. Password reset of a protected account does not remove that second requirement. The [NIST authentication guidance](https://pages.nist.gov/800-63-4/sp800-63b.html) informs password/blocklist and recovery review; no NIST assurance-level claim is made.

## R5. Browser sessions and CSRF

Decision: opaque server-side sessions, hash-only token storage, secure host-only cookies, rotation and absolute/idle deadlines. Use a browser-binding cookie and CSRF token for pre-authentication ceremonies; after login bind CSRF to the session. GET context issuance may set a transport cookie but creates no account, server-side authentication transaction or session. Every mutation verifies token and exact Origin; reject missing Origin on the first-party browser API. See [OWASP session management](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html) and [OWASP CSRF guidance](https://cheatsheetseries.owasp.org/cheatsheets/Cross-Site_Request_Forgery_Prevention_Cheat_Sheet.html).

No wildcard CORS, URL session tokens, shared per-user client defaults or mutation over GET. SSR forwards only the required first-party session cookie to the fixed internal API destination. Personalized payloads and security setup responses are no-store. Alternatives rejected: browser JWTs, localStorage session tokens and forwarding all incoming headers.

## R6. Concurrent changes and external effects

Decision: lock the account row for method/factor changes; use database uniqueness for identities and credential IDs. Atomically update challenge attempts/consumption and rate buckets. Do not hold a transaction open while contacting a provider. PostgreSQL's row-lock behavior supports this design: [PostgreSQL 17 explicit locking](https://www.postgresql.org/docs/17/explicit-locking.html).

Add a claim token to worker leases; complete/fail updates require job ID and current claim token. Delivery jobs carry only a delivery-record ID. A separate record holds encrypted short-lived OTP material and is erased when no longer needed. Use a stable provider idempotency key where supported. With ambiguous provider acceptance and no idempotency/status lookup, mark unknown and do not automatically resend; the user can request a separately budgeted resend. Exactly-once external delivery is not promised. Reserve worst-case permitted SMS cost before send and retain the reservation until outcome is known or reconciled.

Alternative rejected: putting plaintext codes in indefinitely retained job payloads or assuming a database transaction makes an external send exactly once.

## R7. Identity normalization and deployment

Decision: accept explicit international phone input and validate/format it through `github.com/nyaruka/phonenumbers`; reject unsupported regions by configured allowlist. Its [package documentation](https://pkg.go.dev/github.com/nyaruka/phonenumbers) exposes parsing/validation and E.164 formatting. Email comparison lowercases the domain only, preserves local-part case, and performs no dot/plus rewriting. Permit only the reviewed email-address syntax in the API; no implicit provider aliases. Google email remains provider metadata unless separately verified as a Blob email credential.

Use Caddy's local HTTPS facility with a dedicated test origin; install its local trust root only through the existing explicit environment setup process. Host/domain and Google redirect registration remain owner-supplied deployment inputs. The [Caddy documentation](https://caddyserver.com/docs/automatic-https#local-https) documents local certificates and trust-store behavior. Do not substitute insecure cookies or disable WebAuthn origin checks.

Email and SMS use narrow delivery interfaces with deterministic test transports. Live adapters require an explicit provider selection, credentials, allowed destinations and budget. Missing provider configuration disables that delivery method safely and visibly; it does not make Stage 1B's live-provider acceptance pass.

## Dependency and Planning Disposition

The named libraries are selected module choices, not installed dependencies. Exact releases, licenses, Go compatibility and vulnerability results must be recorded when implementation pins them; this research makes no unverified version claims. Existing toolchain pins remain unchanged.

Technical design questions are resolved to concrete reviewable choices in [data-model.md](data-model.md), [the endpoint contract](contracts/identity.md) and [quickstart.md](quickstart.md). Remaining gates are scoped task analysis, live provider/domain setup, dependency pin validation and runtime evidence. None requires adding another framework or reducing 1B scope.