# Blob Constitution

## Core Principles

### I. Specification Before Implementation

Nontrivial changes MUST follow specify, clarify, plan, tasks, analyze, implement and converge.
Specifications describe user outcomes and requirements; plans describe technical choices.
Unknown policy MUST remain distinguishable from an approved decision. Implementation MUST
return contradictions or missing requirements to specification rather than inventing policy.
TDD is not a global requirement: use test-first only when explicitly requested or when it is
the direct way to reproduce a defect. Relevant verification remains mandatory.

### II. Explicit Architecture Boundaries

Business rules MUST reside in the Go modular monolith. Nuxt owns UI, SSR and presentation,
not database access or business policy. Modules MUST expose explicit consumer boundaries;
handlers MUST NOT reach into another module's tables. PostgreSQL is business truth, with
pgx/sqlc and transaction-compatible queries. OpenAPI is the sole API-type contract; generated
Go/TypeScript outputs MUST be reproducible and MUST NOT be edited manually. Do not introduce
an ORM, generic repository, second Go web framework, Redis or microservices into Stage 1.

### III. Security and Privacy Are Functional Requirements

Changes MUST preserve ownership checks, current-state authorization, validation at trust
boundaries, secure credential handling and error handling that prevents data loss. Private
responses MUST NOT enter shared caches. Telemetry MUST use allowlists and exclude secrets
and sensitive user data. Sensitive effects MUST remain safe under retries and concurrency.
Security controls required by an affected feature MUST precede exposing that feature;
future hardening stages do not excuse an insecure intermediate implementation.

### IV. Evidence Before Completion

Verification MUST match the changed behavior: domain tests for nontrivial rules, real
PostgreSQL and migrations for persistence, contract/generation checks for API changes,
and browser/accessibility/RTL checks for critical UI journeys. Do not mock the database.
Record actual commands, outcomes and limitations; a planned check is not a passing check.
Convergence MUST compare code and evidence against the specification, plan and tasks.
No ASVS compliance or scalability claim is permitted without the applicable evidence.

### V. Smallest Complete Scope

Prefer existing project patterns, platform capabilities and installed dependencies.
Introduce new abstractions or dependencies only for a present requirement. Every caller,
fixture, generated artifact, configuration and test affected by a change MUST be handled.
Do not shrink explicitly approved feature scope to make an increment look complete.
Known temporary limits MUST be documented with the condition for revisiting them.

## Project Constraints

- New application work lives under `server/`, `web/` and `api/`; legacy `frontend/` and
  `backend/` MUST NOT be read, moved or changed for the new implementation.
- One public origin fronts web and `/api/v1`. First-party browser authentication uses
  server-side sessions; no browser JWT session substitution.
- Russian, English and Arabic/RTL, current Chrome/Edge/Firefox/Safari, system light/dark
  theme and baseline keyboard/screen-reader accessibility are required.
- Worker effects use PostgreSQL jobs/outbox, bounded execution/retries and idempotency;
  business changes and their queued effects MUST commit atomically.
- Preserve pinned toolchains, lockfiles and immutable CI Action references; keep secrets
  out of source and verification artifacts. No persistent application-local disk state.
- Feature documentation is English. Commits and externally shared artifacts follow the
  repository owner's authorship and Conventional Commit rules, without tool attribution.

## Development Workflow and Quality Gates

Use isolated work for nontrivial implementation and preserve unrelated user changes.
Each stage depends on the previous stage's acceptance evidence. A feature is ready for
implementation only after required product decisions, technical plan and executable tasks
agree. Make assumptions explicit and review them together rather than repeatedly inventing
small policies during coding. Implementation follows the finished specification; unresolved
changes return to planning. Specification/planning and final review follow the configured
strong-model routing; mechanical task generation and implementation follow the configured
Sonnet-class routing. No unavailable model may be silently represented as available.

Local checks MUST cover relevant CI gates: formatting/static analysis, meaningful tests,
generated-code consistency, migrations and production builds. Broader release checks remain
assigned to the stage that owns them, with explicit handoff requirements. Do not claim remote
CI, deployment, provider integration or a restore drill passed from local-only evidence.

## Governance

This constitution codifies the existing owner instructions and accepted architecture. Direct
owner decisions take precedence; conflicting design artifacts MUST then be updated together.
Amendments MUST state the changed rule, rationale and affected specifications/plans. Use a
major version for incompatible principle changes, a minor version for new principles and a
patch version for clarifications. Review constitution compliance during planning and final
convergence. Exceptions require an explicit reason and recorded decision; a process template
cannot override the owner's SDD rules or require artificial red/green cycles.

**Version**: 1.0.0 | **Ratified**: 2026-10-09 | **Last Amended**: 2026-10-09