# Stage 1B Validation Quickstart

Status: validation plan accepted for implementation on 2026-10-10. Identity is not implemented yet. Existing commands below are real repository entry points; Identity-specific expected results describe the future acceptance gate, not checks already passed.

Read [spec.md](spec.md), [data-model.md](data-model.md) and [contracts/identity.md](contracts/identity.md) before running the feature scenarios.

## Prerequisites

- Use the feature worktree and pinned Go/Node/pnpm versions. Preserve unrelated changes and the persistent development database.
- Use the working policy defaults in [plan.md](plan.md). Supply a trusted HTTPS origin and matching WebAuthn RP ID; never bypass TLS/origin checks.
- Set TEST_DATABASE_URL through local secret/environment setup to a disposable PostgreSQL 17.11 database. Never point migration rollback tests at shared or valuable data.
- Use deterministic email/SMS delivery transports in automated tests; their captured messages are private test fixtures, not logs or a public debug endpoint. No paid provider credentials or real recipient messages are required for local verification.
- Live Google/email/SMS checks need explicitly configured client/sender/region/domain credentials and finite cost caps. Until then record live checks as blocked, not passed.

Stage 1A entry evidence combines the [original full-stack smoke](../../docs/verification/stage-1a.md) and [current follow-up](../../docs/verification/stage-1a-followup.md), including successful CI for source HEAD. Do not repeat the full outage exercise without a relevant change.

## Existing Build and Test Commands

From the worktree root in PowerShell, with TEST_DATABASE_URL already configured:

```powershell
corepack pnpm install --frozen-lockfile
corepack pnpm generate
corepack pnpm --dir web check
go -C server tool goose -dir migrations postgres $env:TEST_DATABASE_URL up
go -C server test ./...
go -C server vet ./...
go -C server build ./cmd/api ./cmd/worker
corepack pnpm --dir web test:run
corepack pnpm --dir web build
pwsh ./scripts/check-generated.ps1
pwsh ./scripts/check-actions-pinned.ps1
docker compose config --quiet
```

Expected after implementation: all commands exit zero, database integration tests execute rather than skip, and generation leaves no unintended drift. The existing web test command includes unit and production SSR tests; it is not proof of real-browser authentication by itself. Use the recorded pinned-container substitutes if host native modules are blocked.

Exercise migration down/up only against the disposable test database; confirm both clean creation and upgrade from the accepted 1A schema. Capture versions, source SHA and exit codes without connection strings, environment dumps, cookies or message contents.

## Browser and Security Scenarios

| Scenario | Expected result | Coverage |
| --- | --- | --- |
| Register email -> attempt access -> verify -> login | No full access before verification; duplicate/invalid verification is safe | FR-001/004/005 |
| Enroll first TOTP/passkey after fresh existing-method proof | Confirm new factor before activation; explain backup/loss policy | FR-003/006/007 |
| Require MFA -> login with password/Google/phone -> challenge | First factor alone never grants full access; valid TOTP/passkey completes it | FR-001/006/007 |
| Google login/link with equal email and different subject; replay callback | No merge/takeover; GET callback changes no auth state; POST consumes once | FR-002/003/011 |
| OTP resend, five-way simultaneous consume and exhausted challenge | No reset of attempt budget; one success; expired proof remains invalid | FR-005/012 |
| Validated passkey login; cancel; wrong RP/origin/account; replay | Valid method works; failures never grant access or strand the UI | FR-007/016 |
| Recovery code alone, then first-factor plus code | Code alone denied; combined proof permits only replacement workflow | FR-008/013 |
| Replace lost factor; replay code; lose every factor/code | Recovery revokes old sessions and alerts old channel; replay denied; total loss unrecoverable | FR-008/009/015 |
| Two browsers -> rotate -> revoke device -> revoke all | Old identifiers fail; unrelated device survives targeted revoke; all includes current | FR-009/010 |
| Cross-origin/missing-CSRF mutation; unknown property; oversized body | Rejected consistently before state change | FR-011/013 |
| Users A/B/anonymous through SSR and API | No shared private responses, hydration secrets or cross-owner records | FR-014 |
| Provider failure/ambiguous send, stale worker lease and spend exhaustion | Bounded retry, no stale completion, no uncontrolled resend/charge | FR-012/015 |
| All flows in RU/EN/AR with keyboard and screen reader | Labels, focus, status and Arabic RTL remain usable | FR-016 |

Use a small Playwright suite for repeatable Chromium/Firefox/WebKit coverage and virtual-authenticator cases, plus actual current Chrome/Edge/Firefox/Safari checks for platform passkeys. Playwright's engines/channels are documented in its [official browser guide](https://playwright.dev/docs/browsers); WebKit automation is not a claim that real Safari passkey integration passed. Pin the test dependency and browser binaries during implementation; no missing script is presented here as runnable today.

Run secret canaries through API, SSR, worker, OIDC callback, password-range lookup and provider failures. Assert no test password, full hash/prefix, code, TOTP secret, token, email/phone or callback query appears in captured telemetry. Record only pass/fail and safe request IDs.

## Completion Evidence

Map results to FR-001 through FR-016 and SC-001 through SC-006. Record local tests, real-provider checks and manual browser checks separately. Missing external credentials/domains remain explicit acceptance limitations; local deterministic delivery never counts as real email/SMS delivery. No implementation is complete while required security/race cases fail.

Start/stop only the designated local stack; ordinary shutdown uses `docker compose down` without `--volumes`. No session data, provider secret or recovery material belongs in the evidence file.