# Stage 1A verification

Verified on 2026-09-27 against source commit `5487eec3415a4e663a4c7c10f31a50a2fe878b89` on Windows NT 10.0.26200.0. Docker Engine/CLI 29.6.2 and Compose 5.3.1 ran Linux containers. The selected Go toolchain was 1.27.1; the web test image used Node 24.21.0 and pnpm 11.26.0; PostgreSQL reported 17.11.

The Windows host has Node 24.19.0 and a global pnpm 11.21.0 shim. `corepack pnpm` selects pinned pnpm 11.26.0, but root scripts invoke the older shim internally. Windows Application Control also blocks Rollup's native module. The root script stages were therefore run directly with pinned pnpm, with web checks and tests in the cached Linux test image. The `docker` commands below used Docker Desktop's installed CLI path because it is absent from `PATH`.

| Command or equivalent | Exit | Result |
| --- | ---: | --- |
| `corepack pnpm install --frozen-lockfile` | 0 | Lockfile up to date; pnpm 11.26.0. |
| `corepack pnpm generate` | 1 | Nested host pnpm 11.21.0 rejected the 11.26.0 project pin. |
| `corepack pnpm --dir web generate:api`; `go -C server generate ./...` | 0; 0 | TypeScript and Go generation completed; no tracked drift. |
| `corepack pnpm check`; `corepack pnpm test` | 1; 1 | Nested host pnpm shim rejected the pin before either script stage ran. |
| `corepack pnpm --dir web check` on Windows | 1 | Rollup native module blocked by Windows Application Control. |
| `docker run --rm blob-stage-1a-web-test:latest pnpm --dir web check` | 0 | Nuxt typecheck passed under Node 24.21.0. |
| `docker run --rm blob-stage-1a-web-test:latest pnpm --dir web test:run` | 0 | 14 unit tests and 1 SSR test passed. |
| `go -C server vet ./...`; `go -C server build ./cmd/api ./cmd/worker` | 0; 0 | Go static check and both builds passed with the selected 1.27.1 toolchain. |
| `go test ./...` in the pinned Go 1.27.1 test image against a disposable, migrated PostgreSQL 17.11 database | 0 | All Go packages passed, including database integration tests. |
| `docker compose config --quiet`; `docker compose up --build -d` | 0; 0 | Configuration valid; PostgreSQL healthy and API, worker, web, and proxy running. The web production build completed. |

At the public origin `http://127.0.0.1:8088`, liveness, readiness, and `/` returned HTTP 200; both health responses had `status=ok`. After `docker compose stop postgres` (exit 0), liveness stayed 200 and readiness returned 503 with `dependency_unavailable` and the generic `Service is not ready` message. The response contained no connection details. PostgreSQL was restarted and healthy; the public origin returned 200/200/200 again.

The worker consumed one fixed-ID `system.noop` job (`f318a169-861c-4f7d-903a-9ac5a44a1e10`). Its final database row was `status=done`, `attempts=1`. No job payload was read or logged. The worker process was restarted after the deliberate database outage before inserting the job.

`docker compose down` exited 0 without `--volumes`. No Compose containers remained, and the `blob-stage-1a_postgres-data` volume remained available. The disposable integration-test database was stopped and removed separately.

Known limitation: production PaaS deployment is deferred until plan 1G. Remote GitHub Actions execution was not available in this local verification.
