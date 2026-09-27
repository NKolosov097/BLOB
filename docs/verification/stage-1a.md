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
| `docker run --rm --workdir /src blob-stage-1a-web-test:latest pnpm --dir web check` | 0 | Nuxt typecheck passed under Node 24.21.0. |
| `docker run --rm --workdir /src blob-stage-1a-web-test:latest pnpm --dir web test:run` | 0 | 14 unit tests and 1 SSR test passed. |
| `go -C server vet ./...`; `go -C server build ./cmd/api ./cmd/worker` | 0; 0 | Go static check and both builds passed with the selected 1.27.1 toolchain. |
| `go test ./...` in the pinned Go 1.27.1 test image against a disposable, migrated PostgreSQL 17.11 database | 0 | All Go packages passed, including database integration tests. |
| `docker compose config --quiet`; `docker compose up --build -d` | 0; 0 | Configuration valid; PostgreSQL healthy and API, worker, web, and proxy running. The web production build completed. |

Reproduce the Linux substitute checks from the repository root in PowerShell. The test images come from this checkout's existing Dockerfile targets. The Go database is disposable, has no published port or volume, and uses trust authentication only on its temporary internal network.

```powershell
$docker = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources\bin\docker.exe'
$server = (Resolve-Path .\server).Path
& $docker build --target build -f web/Dockerfile -t blob-stage-1a-web-test:latest .
& $docker run --rm --workdir /src blob-stage-1a-web-test:latest pnpm --dir web check
& $docker run --rm --workdir /src blob-stage-1a-web-test:latest pnpm --dir web test:run
& $docker build --target go-base -f server/Dockerfile -t blob-stage-1a-go-test:latest server

& $docker network create --internal blob-stage-1a-verify
& $docker run --rm -d --name blob-stage-1a-verify-postgres --network blob-stage-1a-verify --env POSTGRES_HOST_AUTH_METHOD=trust --env POSTGRES_USER=blob --env POSTGRES_DB=blob postgres:17.11-alpine
for ($i = 0; $i -lt 30; $i++) {
  & $docker exec blob-stage-1a-verify-postgres pg_isready -U blob -d blob 2>$null | Out-Null
  if ($LASTEXITCODE -eq 0) { break }
  Start-Sleep -Seconds 2
}
if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL not ready' }
$env:TEST_DATABASE_URL = 'postgres://' + 'blob@blob-stage-1a-verify-postgres:5432/blob?sslmode=disable'
& $docker run --rm --network blob-stage-1a-verify --mount "type=bind,source=$server,target=/src,readonly" --env TEST_DATABASE_URL blob-stage-1a-go-test:latest go tool goose -dir migrations postgres $env:TEST_DATABASE_URL up
if ($LASTEXITCODE -ne 0) { throw 'Migration failed' }
& $docker run --rm --network blob-stage-1a-verify --mount "type=bind,source=$server,target=/src,readonly" --env CI=true --env TEST_DATABASE_URL blob-stage-1a-go-test:latest go test ./...
if ($LASTEXITCODE -ne 0) { throw 'Go tests failed' }
& $docker stop blob-stage-1a-verify-postgres
& $docker network rm blob-stage-1a-verify
Remove-Item Env:\TEST_DATABASE_URL
```

At the public origin `http://127.0.0.1:8088`, liveness, readiness, and `/` returned HTTP 200; both health responses had `status=ok`. After `docker compose stop postgres` (exit 0), liveness stayed 200 and readiness returned 503 with `dependency_unavailable` and the generic `Service is not ready` message. The response contained no connection details. PostgreSQL was restarted and healthy; the public origin returned 200/200/200 again.

The worker consumed one fixed-ID `system.noop` job (`f318a169-861c-4f7d-903a-9ac5a44a1e10`). Its final database row was `status=done`, `attempts=1`. No job payload was read or logged. The worker process was restarted after the deliberate database outage before inserting the job.

`docker compose down` exited 0 without `--volumes`. No Compose containers remained, and the `blob-stage-1a_postgres-data` volume remained available. The disposable integration-test database was stopped and removed separately.

Known limitation: production PaaS deployment is deferred until plan 1G. Remote GitHub Actions execution was not available in this local verification.
