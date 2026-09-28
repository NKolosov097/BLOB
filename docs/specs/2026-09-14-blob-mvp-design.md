# Blob MVP — Architecture Specification

Date: 2026-09-14<br>
Status: agreed design, pending final review<br>
Document type: overall architecture and roadmap<br>
Next implementation plan: Stage 1 only (Foundation + Social Core)

## 1. Purpose

Blob is a learning project: a Facebook-style social network built by one developer for a portfolio. The project should provide hands-on practice in Go, backend development, system design, essential DevOps, and a modern Vue/Nuxt frontend.

Priorities:

1. Engineering quality and learning.
2. A working public product that can be shown to friends and colleagues.
3. Room to evolve without a premature move to microservices.
4. Low initial operating cost: preferably free tiers, with up to USD 20–50 per month acceptable.
5. A target validation load of around 1,000 concurrently active users after the functional MVP is complete.

Constraints:

- one developer, 5–10 hours per week, with no hard deadline;
- responsive web first, native apps later;
- existing repository code is considered a previous attempt and does not dictate the architecture;
- the project does not claim a commercial model;
- the MVP neither asks for age nor imposes age restrictions;
- a separate legal review of protections for minors is required before broad public promotion.

## 2. Scope of This Document

The system is too large for a single safe implementation plan. This document therefore establishes the overall architecture, product rules, and stage sequence. Each stage gets its own detailed specification and plan before implementation. Only Stage 1 is planned first.

The functional MVP is complete after Stage 4. Stage 5 validates the portfolio project's scalability and operational readiness.

## 3. Stages

### Stage 1. Foundation + Social Core

- monorepo;
- Nuxt 4 + Vue + TypeScript;
- modular Go backend and a separate Go worker built from the same codebase;
- PostgreSQL, migrations, `pgx` + `sqlc`;
- OpenAPI-first REST API;
- registration, sign-in, recovery, 2FA, passkeys, sessions, and devices;
- profiles, friendships, blocks, and contact import;
- posts, images, original videos, comments, reactions, and reposts;
- an algorithmic feed without ML;
- user and hashtag search;
- basic moderation, decisions, and appeals;
- an in-app new-post indicator via HTTP polling;
- CI, PaaS deployment, backups, logs, metrics, tracing, and basic protection.

### Stage 2. Stories + Notifications + Real-time foundation

- 24-hour stories;
- story audiences, exclusions, views, reactions, and replies;
- in-app notifications and Web Push;
- WebSocket gateway;
- Redis for presence, cross-instance Pub/Sub, transient state, and distributed rate limits;
- a WebSocket new-post feed indicator in place of polling.

### Stage 3. E2EE Messaging

- a mandatory technical spike of a browser-compatible cryptographic SDK;
- 1:1 E2EE chat;
- multiple devices, device revocation, and key-change warnings;
- adding a new device via a trusted device's QR code or an independent recovery secret;
- encrypted history backup;
- text, images, and video;
- delivery and read receipts, typing, and presence;
- editing with a complete version history;
- deletion for oneself and best-effort deletion for both parties;
- reporting with voluntary disclosure of the selected message, explicitly selected context, and every revision of the selected message.

### Stage 4. Calls

- 1:1 WebRTC audio/video;
- screen sharing;
- direct P2P with automatic TURN fallback;
- maximum call duration of 60 minutes;
- call history;
- local recording only after both participants give explicit consent.

### Stage 5. Scale verification

- load testing up to the target of 1,000 concurrent users;
- measurement-driven SQL and index optimization;
- Redis caching only for confirmed bottlenecks;
- testing WebSocket, TURN, uploads, and the worker under load;
- restore drill PostgreSQL;
- DDoS and denial-of-wallet scenarios;
- a practice migration of containers from PaaS to a VPS.

## 4. Technical Architecture

### 4.1. Overview

```text
                              ┌────────────────────────────┐
                              │ CDN / WAF / DDoS protection│
                              └─────────────┬──────────────┘
                                            │ one public origin
                         ┌──────────────────┴──────────────────┐
                         │                                     │
                  pages / assets                          /api/v1, /ws
                         │                                     │
                  ┌──────▼──────┐                       ┌──────▼──────┐
                  │ Nuxt 4 web  │                       │ Go modular  │
                  │ SSR + SPA   │                       │ monolith API│
                  └──────┬──────┘                       └───┬─────┬───┘
                         │ server-side API calls             │     │
                         └────────────────────────────────────┘     │
                                                                  │ outbox/jobs
                 ┌────────────────────┬──────────────────────┬─────▼──────┐
                 │                    │                      │ Go worker  │
          ┌──────▼──────┐      ┌──────▼──────┐       ┌──────▼──────────┐
          │ PostgreSQL  │      │ Object      │       │ Redis (stage 2)│
          │ truth/jobs  │      │ storage/CDN │       │ realtime only │
          └─────────────┘      └─────────────┘       └─────────────────┘

          External commodity services: email, SMS, Web Push, STUN/TURN, OTLP.
```

### 4.2. Why a Modular Monolith

A single Go API process provides simple local transactions, one deployment path, and minimal operating costs. Internally, the code is divided into business modules with explicit APIs; handlers do not access other modules' tables directly.

Microservices are deferred because they would introduce network failures, distributed transactions, separate deployment pipelines, and observability overhead before there is a measurable need. If a module later needs independent scaling, it can be extracted along its existing boundary and outbox contracts.

### 4.3. Components

| Component | Responsibility | Dependencies |
|---|---|---|
| Nuxt web | UI, SSR, SEO, accessibility, localization, client state | Go API; browser APIs |
| Go API | business rules, authorization, REST, WebSocket signaling | PostgreSQL; object storage; Redis from Stage 2 |
| Go worker | outbox, email/SMS, media processing, cleanup, notifications | PostgreSQL; object storage; external providers |
| PostgreSQL | sole source of business truth; sessions; jobs/outbox | managed backup |
| Object storage | original media and variants; E2EE ciphertext attachments | signed URLs; CDN |
| Redis | presence, Pub/Sub, transient call state, distributed limits | introduced only in Stage 2 |
| CDN/WAF | public caching, filtering, DDoS protection | origin protected against direct bypass |
| STUN/TURN | WebRTC connection establishment and relay | short-lived credentials |

Nuxt contains no business rules and does not access the database. The worker is built from the same Go monorepo but runs as a separate process.

## 5. Go Backend Modules

- **Identity** — users, sign-in methods, sessions, devices, 2FA, passkeys, account recovery and deletion.
- **Profiles** — profiles, work experience, education, links, and visibility settings.
- **Friends** — requests/friendships, blocks, contact import and matching.
- **Content** — posts, media, comments, reactions, reposts, mentions, hashtags, and geolocation.
- **Feed** — candidates, access rights, ranking, deduplication, and cursor pagination.
- **Search** — users and hashtags.
- **Notifications** — in-app and Web Push.
- **Moderation** — reports, assignments, evidence, decisions, restrictions, appeals, and audit.
- **Stories** — stories, audiences, views, and reactions.
- **Messaging** — conversation metadata, ciphertext, revisions, receipts, device keys, and prekeys.
- **Calls** — signaling, call state, TURN authorization, and call history.
- **System** — jobs, outbox, health, quotas, and operational controls.

Go does not introduce a generic Repository interface over `pgx`. `sqlc` generates type-safe queries, and its small `DBTX` works with both a pool and a transaction. Interfaces are introduced only at real consumer boundaries, such as email delivery, object storage, and a clock in testable domain logic.

## 6. Web and API

### 6.1. Frontend

- Nuxt 4, Vue, and TypeScript;
- responsive web for current Chrome, Edge, Firefox, and Safari;
- SSR for public profiles and posts;
- Russian, English, and Arabic, with full RTL behavior;
- light/dark theme follows the system preference;
- baseline accessibility is required: semantic HTML, keyboard navigation, focus, labels, contrast, and screen-reader announcements.

Nuxt was chosen because SSR/hybrid rendering suits both public and interactive pages and Vue/Nuxt broadens the developer's experience beyond the React ecosystem, not because of any universal benchmark advantage.

### 6.2. API contract

- REST JSON under `/api/v1`;
- OpenAPI is the source of truth;
- `oapi-codegen` generates strict Go types and interfaces for standard `net/http`;
- the TypeScript client is generated from the same contract;
- no Go web framework is needed initially;
- UUIDv7 values are created by the Go application;
- timestamps are transmitted in UTC ISO 8601;
- all lists have stable ordering.

The public read-only API serves public profiles, posts, and hashtags without developer keys. Responses are cached only after confirming the object is truly public, and the API has separate rate limits. Private and write endpoints require a first-party session.

### 6.3. Pagination and Concurrent Changes

- cursor pagination: feed, comments, messages, notifications, call history, and long search results;
- offset pagination: small administrative tables;
- a cursor includes a stable sort key and UUIDv7 tie-breaker, is signed by the server, and is opaque to the client;
- create endpoints accept an `Idempotency-Key` bound to the user, operation, and body hash;
- reusing the same key with a different body is rejected;
- edit endpoints use a version/ETag and return a conflict for stale changes.

### 6.4. Errors

The API returns a consistent object:

```json
{
  "error": {
    "code": "post_version_conflict",
    "message": "Post was changed",
    "request_id": "...",
    "fields": {}
  }
}
```

`code` is stable and used by the client; `message` is safe to display or localize; internal stack traces and SQL are never returned. Retries are allowed only for idempotent requests and transient errors, with bounded exponential backoff and jitter.

## 7. Identity and Sessions

### 7.1. Sign-in Methods

- email + password;
- Google OIDC;
- phone + OTP;
- passkeys;
- one account can have multiple linked sign-in methods.

A Google identity is determined only by the verified `issuer + subject` pair; matching email addresses never merge accounts automatically. Linking or unlinking a sign-in method requires recent reauthentication with an existing method and verification of the new method. The last working sign-in method cannot be removed.

### 7.2. Server-side session

```text
Successful login
   │
   ├─ Go generates a random session token
   ├─ PostgreSQL stores only token hash + user/device/expiry/revoked
   └─ browser receives a __Host- cookie: Secure, HttpOnly, SameSite

API request
   │
   ├─ browser sends the cookie automatically
   ├─ Go hashes the token and looks up the active session
   └─ policy engine checks the action on the object
```

Browser sessions do not use JWT. Session tokens contain no user data. The session ID rotates after login, MFA, and privilege elevation. Sessions have idle and absolute TTLs, with revocation for a specific device or all sessions.

State-changing requests are protected by a CSRF token and an `Origin` check; CORS permits only exact trusted origins. GET does not change state.

### 7.3. Authentication security

- Argon2id with parameters benchmarked on a production-class instance;
- at least 15 characters for password-only sign-in;
- maximum password length of at least 64 characters; reject known compromised passwords;
- email/phone OTP is purpose-bound, has a TTL and attempt limit, and is consumed atomically;
- resending an OTP does not reset the attempt limit;
- the TOTP secret is encrypted, and recovery codes are hashed and single-use;
- phone-only login is allowed;
- exports, account deletion, linking new identities, and security-setting changes require a passkey or TOTP;
- SMS recovery must not silently disable existing MFA.

### 7.4. Account Deletion

After a deletion request, the account is hidden and sessions are revoked. The owner can cancel deletion within 30 days. After that period, the profile, posts, comments, reactions, stories, and media are deleted; required audit records are pseudonymized but are not called anonymous without proof of irreversibility.

Delivered E2EE messages remain with conversation partners under the name “Deleted user.” A protected deletion journal is reapplied after a restore so that a backup cannot resurrect deleted data.

Temporary account deactivation is not included in the MVP.

### 7.5. Data Export

Export starts asynchronously after recent reauthentication, is rate-limited, and is delivered through a short-lived link with another ownership check. The server-side archive includes the profile, posts, connections, and activity available to the user. CSV values beginning with `=`, `+`, `-`, or `@` are neutralized against spreadsheet formula injection.

Plaintext E2EE conversations are excluded from the server-side archive. Users export them separately on a trusted device: ciphertext is downloaded, decrypted locally, and packaged without sending plaintext to the server.

## 8. Profiles, Friendships, and Privacy

A profile contains a name, username, avatar, cover image, bio, city, birthday, work, education, and links. Arbitrary user-defined fields are deferred.

### 8.1. Visibility

- a new profile is public and indexable;
- the owner can switch the profile to “friends only”;
- a minimal `name + username + avatar` card remains available where identification is needed;
- everyone can see the friend list on a public profile;
- only friends can see the friend list on a friends-only profile;
- a public post remains public even when the profile is friends-only; only the author's minimal card appears beside it;
- changing profile privacy does not change the audience of existing posts.

### 8.2. Friendship and Blocking

Friendship is mutual. The database stores one canonical pair record per pair of users. A block takes precedence over friendship, content audience, story audience, and recommendations.

While signed in, blocked users cannot see each other's content, shared friendships, or mutual activity. Truly public content may still be available to a blocked user after signing out; the UI does not promise absolute concealment of public information.

### 8.3. Contacts

Import supports CSV, Google Contacts, and the browser Contact Picker where available. Discoverability by email or phone is off by default and must be explicitly enabled by the account owner.

Import is one-time: source data and unmatched entries are deleted immediately after matching. Only the minimal match result is retained. A phone-number hash is not treated as anonymization. Batch and daily limits prevent enumeration.

## 9. Content and Media

### 9.1. Posts

- text, images, and video;
- max image: 20 MB;
- max video: 1 GB;
- audience: public, friends, or self;
- a new account's first post is public;
- afterward, the composer uses the user's last audience and always shows it before publication;
- edits display `Edited` and the time of the last change;
- full revision histories of posts and comments are not stored;
- mentions, hashtags, and geolocation;
- reactions: like, love, laugh, wow, sad, angry;
- comments have technically unlimited depth through `parent_comment_id`, but the UI stops increasing indentation after several levels;
- deleting a comment logically hides its entire subtree immediately, while physical cleanup runs in chunks;
- deleting a parent deletes its entire subtree;
- a repost can add its own text but cannot broaden the original's audience;
- if the original is deleted or becomes inaccessible, the original attachment disappears while the repost's own text remains.

Everyone with current access to the source object can see reaction counts and the list of users who reacted. Users who have blocked each other are filtered out.

### 9.2. Geolocation

The user can select a place or precise coordinates. Browser permission does not replace a visible, removable geotag in the composer. Precise coordinates are stored only with the content, excluded from logs, traces, and analytics, and deleted with the object. Analytics receives at most an explicitly selected city or a coarse region.

### 9.3. Standard Media Upload

```text
1. Go creates an upload record: owner, purpose, quota, random object key, expected size/hash.
2. The client receives a short-lived signed upload URL.
3. The client uploads the object directly to a quarantine bucket/prefix.
4. The client confirms the upload.
5. The worker checks size, magic bytes, decoding, and limits.
6. The image is decoded and re-encoded without EXIF; variants are created.
7. The validated immutable version is published under the final key.
```

SVG is not accepted. The unprivileged parser worker has CPU/RAM/time limits, minimal credentials, and restricted network egress. For original video, the MVP validates container/codec/duration/dimensions/metadata but does not transcode it.

Private media is served only via signed URLs with a TTL of no more than 5 minutes, following a current ACL check. After an audience change, no new URLs are issued, and old ones expire within 5 minutes. Public media gets a CDN purge on deletion or visibility change.

### 9.4. Stories

- a photo or video that expires after 24 hours;
- the initial audience is public;
- public or friends is selected separately for each story;
- specific users can be excluded from either audience;
- settings are independent of profile visibility;
- a block always takes precedence;
- viewers see reactions, who reacted, and the total view count;
- only the author sees the full viewer list;
- replies are unavailable in Stage 2. After Messaging is introduced, a story reply creates an E2EE message in a 1:1 conversation.

## 10. Feed and Search

### 10.1. Candidate Sources

Initial configurable mix:

- approximately 70% — the user's own posts and friends' posts;
- approximately 20% — accessible posts with which friends have interacted;
- approximately 10% — globally public posts.

These percentages are a target for candidate selection, not a strict guarantee for every page. ACL checks run before ranking.

### 10.2. Ranking Without ML

```text
score = affinity(viewer, author)
      × interaction_weight(event)
      × time_decay(age)
      + small_quality_adjustments
```

Affinity is based on friendship and past meaningful interactions. Reactions, comments, reposts, and views have distinct configurable weights. A view lasting roughly 2–3 seconds provides a very small positive signal. An ordinary impression supports deduplication and analytics but is not treated as interest.

The Core uses fan-out-on-read: candidates are assembled when the feed opens. Fan-out-on-write or a hybrid approach is introduced only after load tests show read amplification is a measurable problem.

An open feed does not insert new posts automatically. The Core periodically checks for updates over HTTP and shows a “New posts” button. After Stage 2, the event arrives over WebSocket; the user still chooses when to apply the update.

### 10.3. Search

The MVP searches users and hashtags. The server does not index the text of public posts or private messages.

Required TODO after Messaging: full-text search across the entire restored E2EE history. The device downloads ciphertext page by page, decrypts it locally, and builds an incremental index in IndexedDB. Queries are not sent to the server. A new device rebuilds the index; progress and incomplete coverage are shown explicitly; logout/revocation clears keys and the local index. The first version may scan downloaded messages linearly, adding a separate index after measuring latency.

## 11. Notifications and Real Time

In-app notifications and Web Push are supported; user-facing email notifications are not sent. Email remains available for sign-in, verification, recovery, and security alerts.

The WebSocket handshake validates the exact `Origin` and session. Each subscription and action is authorized separately. Revocation, bans, blocks, and loss of access immediately close affected subscriptions. Tokens are never passed in a query string. Connection/message limits, heartbeats, backpressure, and a bounded outbound queue are required.

From Stage 2 onward, Redis is used only for:

- presence;
- Pub/Sub across multiple WebSocket instances;
- transient call state;
- distributed rate limits.

Business truth and durable notifications remain in PostgreSQL.

## 12. E2EE Messaging

### 12.1. Threat model

E2EE must prevent message plaintext from being read if the network, PostgreSQL, object storage, or ordinary Go server is compromised. It does not protect against:

- a malicious extension or malware on the device;
- a compromised browser runtime;
- a malicious recipient, screenshot, or saved file;
- a server that delivers a modified JavaScript client to the user.

Stronger protection against compromised web-client delivery requires a signed native client or independently verifiable code delivery and is deferred.

### 12.2. Protocol constraint

Custom cryptography is prohibited. Before Stage 3, a spike must confirm:

- a supported browser runtime;
- the SDK license and maintainability;
- Signal-class properties: PQXDH/X3DH-class session establishment, Double Ratchet, and multi-device session management;
- official test vectors;
- ratchet-state storage and atomic updates;
- handling of out-of-order, duplicate, and lost messages;
- a security update procedure.

If no supported SDK exists, Stage 3 remains blocked until a safe platform is chosen; it must not be implemented with custom primitives. The Go server stores ciphertext and key-distribution metadata but does not perform client encryption.

### 12.3. Devices and History

- each device has an identity key and device ID;
- adding a device requires QR confirmation by a trusted device or an independent recovery secret;
- account password/SMS recovery does not reveal the E2EE backup key;
- users can view devices and key changes and revoke a device;
- manual safety-number/QR verification is available;
- key transparency is deferred;
- encrypted backup prevents rollback or reuse of ratchet state;
- losing all devices and the recovery secret makes history unrecoverable.

### 12.4. Messages and Revisions

- only 1:1 conversations;
- the server stores ciphertext, envelope metadata, delivery receipts, and encrypted revisions;
- the author's UI shows previous revisions;
- the recipient's UI shows the current version and an `Edited` marker;
- previous encrypted revisions remain on participants' devices to support reports;
- the sender can delete a message for both parties without a time limit, but this is best-effort: a modified or offline client may retain a copy;
- delete-for-self removes local visibility for the particular participant;
- messages and attachments are excluded from logs, traces, and analytics.

### 12.5. E2EE attachments

Before upload, the browser:

1. checks the allowed type and size;
2. removes EXIF from images and creates a safe preview;
3. encrypts the file and preview with distinct random keys/nonces according to the selected SDK;
4. uploads ciphertext directly via a signed URL;
5. sends keys only inside the E2EE message envelope.

The server validates only ciphertext size, quota, checksum, and storage integrity. It cannot scan for viruses or inspect plaintext. The recipient treats the decrypted file as untrusted: format allowlist, safe rendering, no SVG/HTML, and download isolation.

### 12.6. Reports

```text
1. The user selects a specific message.
2. The client shows an exact preview of the data to be disclosed.
3. The client decrypts the selected message locally.
4. For an edited message, all its revisions and timestamps are disclosed automatically.
5. The user separately selects neighboring messages for context.
6. The client sends plaintext/evidence to Moderation over TLS.
7. The server verifies participation, the existence of `message_id`, and delivery.
8. The moderator sees only the submitted evidence.
```

Meta-style message franking is not included in the MVP. The server can therefore verify metadata but cannot cryptographically prove that the submitted plaintext corresponds to stored ciphertext. Evidence is clearly labeled as voluntarily supplied by the user, not as conclusive proof. The rest of the conversation remains inaccessible.

## 13. Calls

Authenticated WebSocket is used only for signaling. Go checks calling permissions and issues short-lived TURN credentials. The WebRTC DTLS fingerprint is bound to the verified signaling transcript. Audio/video is protected by DTLS-SRTP.

ICE order:

1. attempt direct P2P;
2. automatic TURN fallback if a direct connection is impossible;
3. relay-only to hide IP addresses is a deferred privacy setting.

Calls are forcibly ended after 60 minutes. Initial limits allow up to two hours of TURN traffic per account per day, plus concurrent/global limits. Call history contains participants, status, timestamps, and duration, but no content.

Local recording starts only after a consent event from both participants. The server does not record calls. A modified client can still record the screen; the interface does not promise an absolute technical prohibition.

## 14. Jobs, Outbox, and Consistency

PostgreSQL contains `jobs` and `outbox_events`. A business change and its outbox event are committed in one transaction. The worker polls the tables and processes items at least once; exactly-once processing is not promised.

Each handler:

- is idempotent;
- has bounded retries and a dead-letter/failure state;
- rechecks current owner/audience/block/deletion state before an external effect;
- does not publish a notification or media after access is revoked;
- limits batch size, CPU, concurrency, and execution time.

Database constraints and transactions prevent duplicate friendships and reactions, repeated OTP consumption, quota overruns, and moderation conflicts.

## 15. Data

### Identity

`users`, `login_identities`, `sessions`, `devices`, `passkeys`, `totp_credentials`, `recovery_codes`.

### Profiles

`profiles`, `work_experience`, `education`, `profile_links`.

### Social graph

`friendships`, `blocks`, `contact_imports`, `contact_matches`.

### Content

`posts`, `post_media`, `comments`, `post_reactions`, `comment_reactions`, `hashtags`, `post_hashtags`, `content_mentions`.

Reactions are separated by object type to allow proper foreign keys; no generic polymorphic reaction table is used.

### Feed

`content_impressions`, `user_interactions`, `trending_scores`.

### Messaging

`conversations`, `conversation_members`, `encrypted_messages`, `message_revisions`, `delivery_receipts`, `device_keys`, `prekeys`, `encrypted_backups`.

### Stories

`stories`, `story_media`, `story_audience_exclusions`, `story_views`, `story_reactions`.

### Moderation

`reports`, `report_evidence`, `report_assignments`, `moderation_decisions`, `appeals`, `audit_log`.

### System

`notifications`, `push_subscriptions`, `calls`, `call_participants`, `jobs`, `outbox_events`, `deletion_ledger`, `upload_records`.

Exact fields, constraints, and indexes are designed in the specification for the relevant stage, rather than in advance for the whole system.

## 16. Moderation

Users can report other users, posts, comments, and messages. Possible measures include warnings, content removal, temporary or permanent account bans, and restrictions on posting, commenting, or messaging.

Roles:

- **User** — ordinary actions and their own appeals;
- **Moderator** — handling assigned reports and removing content;
- **Senior Moderator** — appeals and temporary account bans;
- **Admin** — roles and permanent bans.

Moderators and admins must use 2FA and recent reauthentication for sensitive actions. No one can assign themselves a role or review their own decision or appeal. The powers to assign roles, access evidence, and impose sanctions are separate.

One appeal is allowed per decision within 30 days; a Senior Moderator who did not make the original decision reviews it. Evidence is retained until the case closes and for another 180 days, then deleted. A minimal pseudonymized audit record remains.

The audit log is append-only at the application level: the runtime role lacks `UPDATE/DELETE`. Access is restricted; evidence and plaintext are not copied into the audit log. A separate immutable sink to protect against a DBA is considered after the Core.

Automatic ML moderation is outside the MVP.

## 17. Centralized Authorization

All handlers, the feed, search, SSR, media downloads, notifications, and workers use one `actor + action + object + current state` model. Access cannot be determined solely by a stored link or by state at the time an event was created.

Decision order:

1. account/content deletion or ban;
2. block;
3. owner/admin capability;
4. object audience and exclusions;
5. friendship/current membership;
6. operation-specific permission.

Derived objects do not broaden access: comments, reactions, reposts, mentions, notifications, search results, and media inherit the source object's restrictions. A repost uses the intersection of its own audience and the original's current audience.

The authorization matrix covers owners, friends, strangers, blocked users, moderators, and admins for every read/write action.

## 18. Security Audit

This section incorporates an independent architecture review by a separate agent. Priorities: Critical before the Core begins, High before the affected feature, Medium before the relevant stage launches publicly, and Low before the portfolio release.

OWASP ASVS Level 2 is used as a baseline applicability checklist. Blob does not claim ASVS compliance until all applicable checks have been completed and documented.

| ID | Risk | Required control | Verification / stage |
|---|---|---|---|
| C1 | Account takeover through identity linking | OIDC `issuer+subject`; no automatic merge by email; recent reauthentication; verify new identity; uniqueness; cannot remove last login method | collision, replay, state/nonce, and race tests / Core |
| C2 | IDOR through derived objects | one policy layer for content/media/feed/search/worker; current state; audience intersection | full authorization matrix / Core |
| H1 | Data leak through CDN/Nuxt SSR cache | shared cache for explicitly public data only; private `no-store`; authorize before `304`; purge on public→private/delete/expiry | user A/B/anonymous cache tests / Core |
| H2 | Recovery bypasses MFA | alternative recovery preserves MFA policy; short-lived single-use tokens; old-channel alerts; revoke sessions | recovery abuse suite / Core |
| H3 | Password/OTP attacks | Argon2id; compromised-password list; purpose-bound atomic OTP; attempt/TTL limits; secure TOTP/recovery | brute-force, resend, and concurrent-consumption tests / Core |
| H4 | Session fixation, CSRF, CORS | `__Host-` cookie; rotation; idle/absolute TTL; CSRF + Origin; exact CORS; no state-changing GET | browser integration tests / Core |
| H5 | Stored XSS steals E2EE keys | output escaping; no HTML/SVG uploads; strict nonce/hash CSP; minimal third-party JS | XSS corpus, including moderator UI / Core + Messaging |
| H6 | Mass assignment / injection | request allowlists; server-owned fields; parameterized SQL; allowlisted sorts; no shell interpolation; body limits | fuzz/negative tests / Core |
| H7 | SSRF through links/webhooks | no automatic preview of arbitrary URLs in MVP; fixed destinations/allowlist; block internal/metadata IPs; redirect/DNS checks; egress limits | IPv4/IPv6/rebinding tests / Core |
| H8 | Signed URL replay/overwrite and upload TOCTOU | owner/purpose-bound record; reserved quota; random immutable key; checksum/version; unreadable quarantine | replay, overwrite, wrong-owner tests / Core |
| H9 | Parser/image bombs/active media | format allowlist; decoded limits; re-encode images; no SVG; sandboxed worker; video metadata limits | bomb/polyglot corpus and resource tests / Core |
| H10 | Unsuitable E2EE SDK | mandatory spike; browser/license/update/vector support; no custom primitives | spike gate / Messaging |
| H11 | Key substitution/new-device takeover | safety number/QR; key-change warning; device revoke; short-lived pairing; independent recovery secret; no ratchet rollback | lost-key, clone, rollback, concurrent-state tests / Messaging |
| H12 | E2EE attachments cannot be scanned | client sanitizes/encrypts; server checks ciphertext quota/integrity only; recipient uses safe rendering | malicious decrypted file suite / Messaging |
| H13 | WebSocket hijack/stale permissions | exact Origin; session + per-action authorization; revocation closes channels; limits/backpressure; no URL tokens | cross-origin, revoke, and flood tests / Real-time |
| H14 | Excessive moderator power | mandatory MFA/reauthentication; least evidence; separate role/evidence/sanction powers; no review of own appeal | RBAC matrix and audit tests / Core + Messaging |
| H15 | Contact privacy/enumeration | explicit opt-in discoverability; minimum fields; delete raw data after one match; batch/daily limits | retention and enumeration tests / Core |
| H16 | Supply chain/secrets blast radius | lockfiles; CI actions pinned by SHA; scanners; no secrets for forks; distinct least-privilege credentials; secret rotation; non-root containers | CI policy and secret scan / Core |
| H17 | Origin bypass/infrastructure takeover | close default PaaS hostname and direct origin; trusted proxy headers; valid Host; private DB/OTLP/metrics/admin; MFA for registrar/PaaS; no dangling DNS | direct-origin and DNS inventory tests / Core |
| H18 | Denial of wallet | reserve upload quota; storage/egress/multipart cleanup; TURN/export/global caps; kill switches | budget threshold drills / each stage |
| M1 | Race conditions/job replay | constraints/transactions; request-hash idempotency; at-least-once handlers recheck state | concurrent and crash-recovery tests / Core |
| M2 | Feed/presence/location privacy | ACL before ranking; opt-out across API/WS; precise-location lifecycle; retention for behavioral data | privacy matrix / relevant stage |
| M3 | WebRTC signaling/IP leaks | signaling transcript binds fingerprint; short-lived TURN credentials; spam/duration/cost limits; block enforcement | MITM/replay/block/cost tests / Calls |
| M4 | Sensitive telemetry | field allowlists; collector redaction; no URLs/query/cookies/SQL params/bodies/chat/media URLs; RBAC/retention; untrusted trace headers | synthetic-secret canary test / Core |
| M5 | Audit tampering | DB runtime role cannot mutate; separation of rights; gap detection; later immutable sink | privilege and gap tests / Core |
| M6 | Delete/export/backup inconsistency | retention matrix; immediate hide/revoke; idempotent purge; deletion replay after restore; recent reauthentication/short-lived export link | deletion and restore drills / Core |
| M7 | Spam/scraping/report/feed gaming | multi-key limits; cooldowns; new-account limits; untrusted impression/dwell; reports never count as automatic proof | abuse/load tests / Core |
| L1 | Debug exposure, API inventory, CSV/bidi hazards | inventory public hosts/versions; no public pprof/debug; neutralize CSV `= + - @`; Unicode/RTL spoof tests | release checklist / Core |
| L2 | No incident runbook | runbook: revoke sessions, rotate secrets, disable uploads/SMS/TURN, stop compromised frontend, restore, report vulnerabilities | one tabletop drill / Scale verification |

### 18.1. DDoS and Cost Controls

- edge CDN/WAF/DDoS protection;
- closed origin;
- token buckets by IP, session, user, and endpoint;
- risk-based CAPTCHA only;
- SMS attempt and spending caps;
- HTTP body, header, timeout, concurrency, and DB pool limits;
- PostgreSQL statement timeout;
- worker concurrency and retry caps;
- WebSocket connection/message/heartbeat/backpressure limits;
- upload quota reserved before issuing a signed URL;
- orphan/multipart cleanup;
- alerts at 70% and 90% of the monthly budget;
- at 100%, new uploads/TURN are disabled, while existing content remains readable.

Initial per-user limits: 5 GB total media, 2 GB uploaded per day, two concurrent uploads, and two hours of TURN per day. All values are configurable and reviewed against actual costs.

## 19. Retention and Backup

| Data | Retention / rule |
|---|---|
| PostgreSQL backups | 30 daily copies; RPO up to 24 hours |
| Traces | 7 days |
| Technical logs | 14 days |
| Aggregated metrics | 30 days |
| Moderation evidence | case duration + 180 days |
| Appeal window | 30 days after the decision |
| Account deletion recovery | 30 days |
| Raw/unmatched contacts | deleted immediately after one matching pass |
| Precise geolocation | only for the lifetime of the associated content |
| Stories | unavailable immediately after `expires_at`; physical cleanup is asynchronous |
| E2EE ciphertext/revisions | while at least one participant retains the conversation; physical cleanup within 30 days after both delete it; closed backups expire within 30 days |

Backups are encrypted and unavailable to the application as ordinary data. After a restore, deletion/revocation ledgers are applied before the service opens to traffic. Restore drills are performed regularly and measure actual RPO/RTO. No RTO is promised before the first drill; the measured value becomes the baseline.

## 20. Observability

- structured logs with request/trace IDs;
- health/readiness endpoints;
- error tracking;
- RED metrics for the API, DB pool, worker, WebSocket, uploads, and external providers;
- OpenTelemetry traces from Nuxt SSR → Go → PostgreSQL/worker via OTLP;
- initially 100% of traces, then sampling as costs grow;
- alerts for availability, error rate, latency, queue age, DB saturation, and spending.

Telemetry uses an allowlist. Message/post bodies, coordinates, cookies, tokens, email, phone, SQL parameters, signed media URLs, exception locals, and authentication secrets are prohibited. Incoming trace context is treated as untrusted and normalized.

## 21. Deployment

The first environment is a container-based PaaS:

- a separate Dockerfile for Nuxt, Go API, and the Go worker;
- `compose.yaml` for local development and a future VPS;
- configuration via environment variables/secrets;
- managed PostgreSQL and object storage;
- no mandatory provider-specific APIs in business logic;
- no persistent local disk state.

This contract makes a move to a VPS an operational task: set up a reverse proxy, containers, networking, secrets, backups, and observability. Business code and the API need no rewrite. The migration takes place after the Core as a separate learning exercise, not alongside the first delivery.

## 22. Testing and CI

### 22.1. Test pyramid

- unit tests for domain rules;
- integration tests with real PostgreSQL and real migrations, without a mock database;
- OpenAPI contract tests;
- frontend tests for complex state, RTL, and accessibility;
- a small E2E suite for critical user flows;
- authorization matrix tests;
- E2EE official vectors and multi-device, lost-key, out-of-order, and rollback tests;
- load tests for feed/uploads/WebSocket/messages;
- worker retry/idempotency/crash recovery;
- backup restore drills.

### 22.2. Critical Core E2E Tests

1. register → verify → login → 2FA → logout/revoke device;
2. create a public/friends/self post → verify access as different users;
3. friendship → unfriend/block → immediate loss of derived access;
4. upload image/video → quarantine → publish/fail;
5. report → moderation decision → appeal reviewed by a different moderator;
6. public SSR/cache → private/delete → no leak to another user;
7. request account deletion → recover or final purge → restore backup without data resurrection.

### 22.3. CI gates

- Go/TypeScript formatting and static analysis;
- unit/integration/contract tests;
- OpenAPI generation consistency;
- migration up/down and clean-database tests;
- dependency/license/secret scan;
- container scan;
- production build;
- deployment only from a passing pipeline.

## 23. Explicitly Deferred (TODO)

| TODO | Revisit when |
|---|---|
| Video transcoding, multiple resolutions, thumbnails, and streaming | after measuring playback/storage problems with original files |
| Hybrid/fan-out-on-write feed | after a load test confirms read amplification |
| Redis as a cache | only after profiling a specific hot path |
| Full-text search of public posts | a separate product phase |
| Full-text local search of all E2EE history | after Messaging is stable; mandatory TODO |
| Meta-style message franking | if false E2EE reports become a real problem and a proven protocol is available |
| Key transparency | after multi-device E2EE is stable |
| Protection against compromised web-client delivery | a signed native client or independently verifiable delivery |
| Group chat | after stable 1:1 Messaging |
| Group calls | after stable 1:1 Calls and an SFU assessment |
| TURN relay-only privacy mode | after assessing cost and user demand |
| Custom profile fields | after validating the need beyond a basic profile |
| Custom moderation roles/permissions | after a real moderator team exists |
| Offline send queue | after the Core; ordinary delivery resumes when connectivity returns |
| Bluetooth mesh/chat | an experiment only after native mobile; not a web feature |
| Native iOS/Android | after validating the web product and API |
| Feed transport fanout/microservices/Kafka | only when the modular monolith/outbox reaches a measurable limit |
| Age restrictions, parental consent, and child-safety program | mandatory legal/product review before broad public promotion |

## 24. Stage Completion Criteria

### Social Core ready

- critical Core E2E tests pass;
- the authorization/cache matrix has no known leaks;
- a backup has been successfully restored at least once;
- a trace crosses Nuxt → Go → PostgreSQL/worker;
- budget kill switches have been tested;
- public profiles/posts are correctly SSR-rendered and indexed;
- private/friends/self content does not enter shared caches or search.

### Functional MVP ready

- Stages 1–4 work in a production-like environment;
- the E2EE spike gate passes and protocol vectors pass;
- new/revoked-device, backup-recovery, and lost-key flows are tested;
- an E2EE report discloses only the previewed evidence;
- WebRTC block/replay/TURN/60-minute flows are tested;
- accessibility/RTL checks pass for critical flows.

### Portfolio scale verified

- the load profile and test infrastructure are documented;
- the target of 1,000 concurrent users is tested with a realistic mix of operations, not a single health endpoint;
- the SLO baseline and bottlenecks are published;
- restore and incident tabletop drills are completed;
- known limits and costs are described honestly.

## 25. Accepted Tradeoffs

- A modular monolith instead of early microservices.
- PostgreSQL jobs/outbox instead of Kafka or a separate queue.
- Redis is absent from the Core and introduced only for real-time coordination.
- Fan-out-on-read until a measurable limit is reached.
- Original video without transcoding in the Core.
- E2EE is mandatory for Messaging but cannot use custom cryptography.
- Reports disclose exact user-selected evidence without Meta-style proof.
- Calls use P2P and TURN fallback; mandatory relay-only mode is deferred.
- Age-related logic is absent from the MVP, but this is not a legal exemption.

These choices reduce the scope of the first delivery without weakening security boundaries, privacy checks, data-loss prevention, or accessibility.

## 26. References

- [Meta Engineering: Building end-to-end security for Messenger](https://engineering.fb.com/2023/12/06/security/building-end-to-end-security-for-messenger/)
- [Messenger End-to-End Encryption Overview](https://engineering.fb.com/wp-content/uploads/2023/12/MessengerEnd-to-EndEncryptionOverview_12-6-2023.pdf)
- [The Labyrinth Encrypted Message Storage Protocol](https://engineering.fb.com/wp-content/uploads/2023/12/TheLabyrinthEncryptedMessageStorageProtocol_12-6-2023.pdf)
- [Signal libsignal](https://github.com/signalapp/libsignal)
- [OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)
- [OpenTelemetry](https://opentelemetry.io/docs/)
- [FTC COPPA Rule](https://www.ftc.gov/legal-library/browse/rules/childrens-online-privacy-protection-rule-coppa)
- [GDPR Article 8](https://eur-lex.europa.eu/legal-content/EN/TXT/PDF/?uri=CONSIL%3APE_17_2016_INIT)
- [EU Digital Services Act: protection of minors](https://digital-strategy.ec.europa.eu/en/policies/protecting-young-people-online)
