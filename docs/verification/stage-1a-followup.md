# Stage 1A follow-up verification

Date: 2026-10-09. Source: `9d97aa7cef616988dd24149c54fc1f58982e971c`.
This supplements [the original report](stage-1a.md); it does not replace its historical results.

## Current commit CI

The [successful GitHub Actions run](https://github.com/NKolosov097/BLOB/actions/runs/36416339403)
completed on 2026-09-28 for this exact source commit. Its status and step results were
read again on the date above using `gh run view 36416339403 --repo NKolosov097/BLOB --json jobs,headSha,conclusion`.

The run passed dependency installation, migrations against PostgreSQL 17.11,
generated-code drift checks, Action SHA checks, `pnpm check`, `pnpm test`, both Go
binary builds, the Nuxt production build and Compose configuration validation.
The workflow supplies `TEST_DATABASE_URL` and `CI=true`, so the database tests are
required rather than silently skipped. The original report's lack-of-remote/CI
limitation is historical and no longer applies to this commit.

## Local checks

| Check | Result |
| --- | --- |
| `go -C server test ./...` | Passed after process-scoped Git ownership trust; database tests skipped without `TEST_DATABASE_URL` |
| `go -C server vet ./...` | Passed |
| `go -C server build ./cmd/api ./cmd/worker` | Passed |
| `docker build --target build -f web/Dockerfile -t blob-stage-1a-web-test:latest .` | Passed, including the production build |
| `docker run --rm --workdir /src blob-stage-1a-web-test:latest pnpm --dir web check` | Passed |
| `docker run --rm --workdir /src blob-stage-1a-web-test:latest pnpm --dir web test:run` | Passed: 14 unit tests and 3 production SSR tests |
| `powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/check-actions-pinned.ps1` | Passed; policy override applied to this invocation only |
| `docker compose -p blob-verify-20261009 config --quiet` | Passed |

The host's Git ownership guard initially blocked nested process-canary builds.
Trust was scoped to the verification process; no global Git configuration changed.
Docker Desktop was initially stopped. After startup, slow/stalled operations led
to one restart, interrupting the first web test run and Go test-image export.
The complete web test run above succeeded on retry. The second Go image export
was cancelled after exact-commit CI evidence was confirmed; no local database
integration pass is claimed. The disposable PostgreSQL container and internal
network were removed. No application data volume was deleted.

## Entry gate for Stage 1B

The existing full-stack outage/recovery and tracing evidence, successful CI on the
current source commit, and local checks above support starting Stage 1B planning.
The complete Compose outage/recovery smoke was not repeated in this follow-up.
Production deployment, provider integration and Stage 1B behavior remain unverified.
