# Stage 1B Task Consistency Analysis

Date: 2026-10-10. Inputs: [spec](spec.md), [plan](plan.md), [model](data-model.md), [contract](contracts/identity.md), [tasks](tasks.md), [increment 01](increments/01-sessions.md) and constitution. Spec Kit prerequisite check passed with spec/tasks and all Phase 0/1 artifacts present. No extension hooks exist.

## Scoped Verdict

**T001-T017 are conditionally ready for implementation.** Correct T011's nonexistent path and remove the duplicate TTL configuration from T005 as listed below. The detailed increment brief is authoritative, including fixture-only users, eight endpoints, real database verification and no public login shortcut. The TTL ownership clarification is already recorded in the brief; tasks.md was not edited by this review.

The other 38 tasks are a non-executable backlog, not an approved implementation plan. Correct them mechanically, then produce/analyze a scoped brief before each later increment. Their problems do not require adding future auth features to the isolated session foundation.

## Findings and Exact Corrections

| ID | Severity | Location | Required correction |
| --- | --- | --- | --- |
| A01 | Medium; before slice 1 | T011 | Replace `server/internal/observability/otel_test.go` with `server/internal/platform/observability/otel_test.go`. Keep existing observability assertions |
| A02 | Medium; before slice 1 | T005 | `config.Identity` contains PublicOrigin and BrowserKey only. Remove fixed durations from LoadIdentity; seven/thirty-day constants live once in identity. No new runtime TTL options |
| A03 | High; before slice 2 | T019/T020/T026/T032/T041 | T019 must create/use `login_identities`, `auth_challenges`, `auth_buckets`, `auth_deliveries`, `auth_requests` according to the model, not add email/password to users. T020 queries those records. T026 names `totp_credentials`; T032 names `passkeys`. T041 adds phone support to login_identities and existing challenge/budget records, not a users phone column or parallel schema |
| A04 | Critical; before OIDC/linking | T036 | Extend the existing login_identities model, rather than create `identities`. Verified canonical issuer+subject ownership must be globally unique across accounts, not merely unique per account. Link collision cannot create a second owner |
| A05 | High; before first delivery | T022/T025 | Add explicit handler-registration, claim-token/fenced completion/failure, delivery-record secret encryption/erasure, transactional budget/reservation and enqueue, bounded retry and ambiguous-provider-outcome handling. Paths include `server/internal/modules/system/jobs.go`, `server/queries/system.sql`, migrations/generated db output, Identity delivery logic and worker composition. Test stale worker completion and ambiguous send behavior; database transaction alone is not provider idempotency |
| A06 | High; before usable email flow | T048/T050/T052 | Move email registration/verification/login UI, localized strings, request-scoped authenticated SSR, trusted local HTTPS and its browser smoke into increment 2. Add MFA/passkey/Google/phone UI and locale strings with their own increments. Keep recovery/settings completion in increment 7 and cross-flow convergence in increment 8. Do not postpone the first usable journey or all localization to phases 7/8 |
| A07 | High; before MFA | T023/T029/T030 | Explicitly add MFA-pending challenge/access gating and the session first-factor/strong-purpose/security-version metadata when increment 3 introduces MFA. Adapt login to call Issue only after all required proof. Apply the same gate to later Google/phone login; a first factor alone must not issue a full protected-account session |
| A08 | Medium; before later tasks execute | T018-T055 and Dependencies | Add concrete file paths and story labels to executable later tasks, preserving task IDs when possible. Passkeys depend on the shared bootstrap/step-up machinery from phase 3; do not describe phases 3/4 as independent unless that common prerequisite is explicitly extracted. Each phase needs generated-output and focused verification coverage |

No unrelated cleanup, new provider choice or framework is requested by these corrections. A03-A08 are backlog corrections, not additional T001-T017 scope.

## Increment 01 Coverage

| Requirement | Tasks | Binding acceptance |
| --- | --- | --- |
| FR-009 sessions | T002-T010,T012-T017 | Hash-only tokens, idle/absolute expiry, rotation/revocation races and startup secrecy |
| FR-010 sessions/devices | T001-T004,T007-T010,T015-T017 | Owner isolation, bounded lists/cursors, targeted/all revocation; UI remains next usable journey |
| FR-011 browser request protection | T005/T006/T008-T010/T013/T016/T017 | Exact Origin, session/anonymous CSRF, secure cookies, safe logout retry and body/query validation |
| FR-013 current ownership/state, partial | T002/T003/T007/T009/T015-T017 | Active-state/FK checks, user-first locking and authorizing-session recheck |
| FR-014 privacy/error handling, partial | T006/T008-T017 | No-store, explicit generated-error overrides, telemetry secret canaries; SSR-specific acceptance later |

All five in-scope requirement areas have executable tasks and detailed acceptance in the brief. This is not 100% coverage of the full 16-requirement feature. Broad references to the brief are binding: token-at-rest/FK/rollback/canary checks remain required even if a task summarizes only several of its cases. Preserve the brief's existing google/uuid promotion and all NewHandler callers while implementing T007/T008.

## Gates and Metrics

- 16 feature requirements; 55 listed tasks; 17 reviewed for current execution and 38 backlog tasks not approved.
- Eight findings: two immediate mechanical slice corrections; six later backlog corrections, including one critical global-identity-ownership conflict.
- No current-slice constitution violation after A01/A02 correction. No unmapped task in T001-T017. Full-feature task coverage is not endorsed.
- Real PostgreSQL tests must execute, not skip. Browser/SSR/live-provider outcomes are not claimed for the backend-only slice.
- Proceed with T001-T017 after corrections, verify and converge against increment 01. Do not execute later backlog entries until corrected and analyzed for their own scope.