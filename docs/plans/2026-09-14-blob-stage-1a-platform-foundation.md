# Blob Stage 1A Platform Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a runnable vertical foundation for Blob from scratch: Nuxt web → OpenAPI Go API → PostgreSQL, a separate Go worker, containers, CI, and end-to-end tracing.

**Architecture:** The new implementation lives in `web/`, `server/`, and `api/`; the old `frontend/` and `backend/` are neither read nor changed. The Go API is a modular monolith built on `net/http`; the worker is built from the same Go module. PostgreSQL is the source of truth, OpenAPI is the source of the API contract, and one local reverse proxy gives the web app and `/api` a shared origin.

**Tech Stack:** Go 1.27.1, `net/http`, pgx 5.10.0, sqlc 1.31.1, goose 3.27.3, oapi-codegen 2.8.0, PostgreSQL 17.11, Node 24.21.0 LTS, pnpm 11.26.0, Nuxt 4.5.2, Vue 3, TypeScript, Vitest, OpenTelemetry/OTLP, Docker Compose, Caddy.

**Spec:** `docs/specs/2026-09-14-blob-mvp-design.md`

## Global Constraints

- This plan implements only Stage 1A; auth, profiles, friends, content, feed, search, and moderation get separate plans 1B–1G.
- Do not read, move, or change the old `/frontend` and `/backend`; add them to the root `.gitignore`.
- Nuxt handles only UI/SSR/SEO; business rules reside in Go.
- REST JSON is available under `/api/v1`; OpenAPI is the source of truth.
- Use `pgx` + `sqlc`, real PostgreSQL in integration tests, and no database mocks.
- Do not create a generic Repository interface over `pgx`; use the generated `DBTX`.
- Redis, Kafka, an ORM, a Go web framework, and Kubernetes are excluded.
- All containers run without root where the official image permits it; the application stores no persistent state on local disk.
- Logs and traces use an allowlist; bodies, query values, cookies, credentials, email, phone, and signed URLs are prohibited.
- Current Chrome, Edge, Firefox, and Safari; Russian, English, Arabic/RTL; system light/dark theme; keyboard and screen-reader basics.
- GitHub Actions are pinned to full commit SHAs.
- TDD: failing test → minimal implementation → passing test → focused commit.

---

## Stage 1 decomposition

This is the first of seven independent deliverables:

1. **1A Platform Foundation** — this document.
2. **1B Identity and Sessions** — auth, OIDC, OTP, 2FA, passkeys, recovery, sessions/devices.
3. **1C Profiles and Social Graph** — profiles, friendship, blocks, contacts.
4. **1D Content and Media** — posts, comments, reactions, reposts, uploads.
5. **1E Feed and Search** — candidate selection, ranking, cursors, users/hashtags.
6. **1F Moderation and Account Lifecycle** — reports, appeals, restrictions, export/delete.
7. **1G Core Hardening and Release** — authorization/cache matrix, WAF/origin, load and restore drills, PaaS release.

Each subsequent plan builds on a working result from the previous one. Do not start 1B until this document's acceptance checks pass.

## Target file map

```text
/
├── .editorconfig                         # consistent text settings
├── .gitignore                            # generated/secrets + explicit exclusion of old attempts
├── .go-version                           # Go 1.27.1
├── .node-version                         # Node 24.21.0
├── .env.example                          # safe config names without secrets
├── package.json                          # root orchestration scripts only
├── pnpm-workspace.yaml                   # workspace with web/
├── compose.yaml                          # postgres, api, worker, web, proxy
├── Caddyfile                             # shared origin: /api → Go, everything else → Nuxt
├── README.md                             # Stage 1A startup and verification
├── api/
│   └── openapi.yaml                      # single API contract
├── server/
│   ├── go.mod / go.sum
│   ├── Dockerfile
│   ├── sqlc.yaml
│   ├── migrations/00001_system.sql
│   ├── queries/system.sql
│   ├── cmd/api/main.go
│   ├── cmd/worker/main.go
│   └── internal/
│       ├── app/api.go                    # composition root API
│       ├── app/worker.go                 # composition root worker
│       ├── modules/system/health.go      # liveness/readiness use case
│       ├── modules/system/jobs.go        # claim/complete/fail jobs
│       ├── platform/config/config.go
│       ├── platform/database/postgres.go
│       ├── platform/httpserver/server.go
│       ├── platform/observability/otel.go
│       ├── platform/requestid/middleware.go
│       └── transport/httpapi/            # generated types + strict handlers
├── web/
│   ├── package.json
│   ├── nuxt.config.ts
│   ├── openapi-ts.config.ts
│   ├── Dockerfile
│   ├── app/app.vue
│   ├── app/pages/index.vue
│   ├── app/assets/css/main.css
│   ├── app/composables/useBlobApi.ts
│   ├── app/api/generated/                # committed generated TS client
│   ├── i18n/locales/{ru,en,ar}.json
│   ├── server/plugins/otel.ts
│   └── test/status.test.ts
├── scripts/
│   ├── check-generated.ps1
│   └── check-actions-pinned.ps1
└── .github/workflows/ci.yml
```

`pnpm-lock.yaml` lives once at the repository root. Generated Go and TypeScript files are committed, never edited manually, and checked for drift in CI.

### Scope coverage

| Approved spec concern | Plan that implements it |
|---|---|
| Runtime boundaries, OpenAPI, PostgreSQL, worker, containers, base telemetry | 1A (this document) |
| Identity, server sessions and abuse-safe auth | 1B |
| Profiles, friendships, blocks and contact discovery | 1C |
| Posts, comments, reactions, reposts and media | 1D |
| Feed ranking, cursor pagination and search | 1E |
| Reports, appeals, account export/deletion and moderation | 1F |
| WAF/origin protection, dependency/license/secret/container scans, backup/restore and load proof | 1G |

Stages 2–5 from the approved spec remain separate milestones. This split changes neither their order nor their acceptance criteria.

---

### Task 1: Initialize the clean workspace boundary

**Files:**
- Create: `.gitignore`
- Create: `.editorconfig`
- Create: `.go-version`
- Create: `.node-version`
- Create: `package.json`
- Create: `pnpm-workspace.yaml`
- Create: `.env.example`
- Create: `README.md`

**Interfaces:**
- Consumes: approved design spec only; no old application code.
- Produces: root commands `check`, `test`, `generate`; pinned runtime files; clean Git boundary.

- [ ] **Step 1: Initialize Git and write the boundary files**

Run:

```powershell
git init
git branch -M main
```

Create `.gitignore`:

```gitignore
# Previous attempts: preserved locally, excluded from the new implementation.
/frontend/
/backend/

# Secrets and local config
.env
.env.*
!.env.example

# Generated runtime artifacts
node_modules/
.nuxt/
.output/
coverage/
dist/
*.test
*.out

# Editors / OS
.idea/
.vscode/
.DS_Store
Thumbs.db
```

Create `.editorconfig`:

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = space
indent_size = 2

[*.go]
indent_style = tab

[Makefile]
indent_style = tab
```

Create `.go-version` and `.node-version`:

```text
1.27.1
```

```text
24.21.0
```

- [ ] **Step 2: Add root orchestration without a task runner dependency**

Create `package.json`:

```json
{
  "name": "blob",
  "private": true,
  "packageManager": "pnpm@11.26.0",
  "scripts": {
    "generate": "pnpm --dir web generate:api && go -C server generate ./...",
    "check": "pnpm --dir web check && go -C server vet ./...",
    "test": "pnpm --dir web test:run && go -C server test ./..."
  }
}
```

Create `pnpm-workspace.yaml`:

```yaml
packages:
  - web
```

Create `.env.example`:

```dotenv
APP_ENV=development
HTTP_ADDR=:8080
DATABASE_URL=postgres://blob:blob@postgres:5432/blob?sslmode=disable
API_INTERNAL_BASE=http://api:8080/api/v1
NUXT_PUBLIC_API_BASE=/api/v1
OTEL_EXPORTER_OTLP_ENDPOINT=
OTEL_SERVICE_NAME=
```

- [ ] **Step 3: Write the initial README scope**

Create `README.md` with these exact sections:

```markdown
# Blob

Portfolio social network built as a modular Go monolith with a Nuxt web client.

## Current scope

Stage 1A builds only the platform foundation. Existing `frontend/` and `backend/`
directories are preserved previous attempts and are not part of the new system.

## Prerequisites

- Go 1.27.1
- Node.js 24.21.0 LTS
- pnpm 11.26.0
- Docker with Compose v2

## Local start

Copy `.env.example` to `.env`, then run `docker compose up --build`.
Open `http://127.0.0.1:8088`.

## Verification

- `pnpm generate`
- `pnpm check`
- `pnpm test`
- `docker compose config --quiet`
```

- [ ] **Step 4: Verify the clean boundary**

Run:

```powershell
git status --short
git check-ignore frontend backend
```

Expected: `frontend` and `backend` are reported as ignored; no secrets or `node_modules` are staged.

- [ ] **Step 5: Commit**

```powershell
git add .gitignore .editorconfig .go-version .node-version package.json pnpm-workspace.yaml .env.example README.md docs
git commit -m "chore: initialize Blob workspace"
```

---

### Task 2: Define and generate the health API contract

**Files:**
- Create: `api/openapi.yaml`
- Create: `server/go.mod`
- Create: `server/internal/transport/httpapi/oapi-codegen.yaml`
- Create: `server/internal/transport/httpapi/generate.go`
- Generate: `server/internal/transport/httpapi/api.gen.go`
- Create: `server/internal/transport/httpapi/contract_test.go`

**Interfaces:**
- Consumes: root Go 1.27.1 pin.
- Produces: `httpapi.StrictServerInterface`; operations `GetLiveness` and `GetReadiness`; `HealthResponse` and `ErrorResponse`.

- [ ] **Step 1: Write the OpenAPI contract**

Create `api/openapi.yaml`:

```yaml
openapi: 3.0.3
info:
  title: Blob API
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /health/live:
    get:
      operationId: getLiveness
      tags: [system]
      responses:
        "200":
          description: Process is alive
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"
  /health/ready:
    get:
      operationId: getReadiness
      tags: [system]
      responses:
        "200":
          description: Dependencies are ready
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/HealthResponse"
        "503":
          description: A required dependency is unavailable
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
components:
  schemas:
    HealthResponse:
      type: object
      additionalProperties: false
      required: [status]
      properties:
        status:
          type: string
          enum: [ok]
    ErrorResponse:
      type: object
      additionalProperties: false
      required: [error]
      properties:
        error:
          type: object
          additionalProperties: false
          required: [code, message, request_id]
          properties:
            code: { type: string }
            message: { type: string }
            request_id: { type: string }
            fields:
              type: object
              additionalProperties: true
```

- [ ] **Step 2: Pin the Go module and code generators**

Create `server/go.mod`:

```go
module blob/server

go 1.27.1

tool (
	github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
	github.com/pressly/goose/v3/cmd/goose
	github.com/sqlc-dev/sqlc/cmd/sqlc
)
```

Run from `server/`:

```powershell
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
go get -tool github.com/pressly/goose/v3/cmd/goose@v3.27.3
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go mod tidy
```

- [ ] **Step 3: Configure strict stdlib server generation**

Create `server/internal/transport/httpapi/oapi-codegen.yaml`:

```yaml
package: httpapi
output: api.gen.go
generate:
  models: true
  std-http-server: true
  strict-server: true
  embedded-spec: true
```

Create `server/internal/transport/httpapi/generate.go`:

```go
package httpapi

//go:generate go tool oapi-codegen -config oapi-codegen.yaml ../../../../api/openapi.yaml
```

Run:

```powershell
go -C server generate ./internal/transport/httpapi
```

- [ ] **Step 4: Write the contract validation test**

Create `server/internal/transport/httpapi/contract_test.go`:

```go
package httpapi

import (
	"context"
	"testing"
)

func TestEmbeddedContractIsValid(t *testing.T) {
	spec, err := GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	if err := spec.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 5: Run the contract test**

Run:

```powershell
go -C server test ./internal/transport/httpapi -run TestEmbeddedContractIsValid -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add api server/go.mod server/go.sum server/internal/transport/httpapi
git commit -m "feat: define initial OpenAPI contract"
```

---

### Task 3: Build the Go API process with liveness and safe errors

**Files:**
- Create: `server/internal/platform/config/config.go`
- Test: `server/internal/platform/config/config_test.go`
- Create: `server/internal/platform/requestid/middleware.go`
- Test: `server/internal/platform/requestid/middleware_test.go`
- Create: `server/internal/modules/system/health.go`
- Test: `server/internal/modules/system/health_test.go`
- Create: `server/internal/transport/httpapi/handler.go`
- Test: `server/internal/transport/httpapi/handler_test.go`
- Create: `server/internal/platform/httpserver/server.go`
- Create: `server/internal/app/api.go`
- Create: `server/cmd/api/main.go`

**Interfaces:**
- Consumes: generated `httpapi.StrictServerInterface` and `HealthResponse`.
- Produces: `config.Load() (Config, error)`; `system.HealthChecker`; `app.RunAPI(context.Context) error`; HTTP `GET /api/v1/health/live`.

- [ ] **Step 1: Write failing config tests**

```go
func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	if !errors.Is(err, ErrDatabaseURLRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestLoadUsesBoundedHTTPDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example")
	cfg, err := Load()
	if err != nil { t.Fatal(err) }
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second || cfg.HTTP.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected HTTP defaults: %+v", cfg.HTTP)
	}
}
```

Run:

```powershell
go -C server test ./internal/platform/config -v
```

Expected: FAIL because `Load` is undefined.

- [ ] **Step 2: Implement typed environment config**

Use only `os.LookupEnv`, `strconv` and `time`; do not add a config package. Required fields: `DATABASE_URL`. Defaults: `APP_ENV=development`, `HTTP_ADDR=:8080`, `ReadHeaderTimeout=5s`, `ReadTimeout=15s`, `WriteTimeout=30s`, `IdleTimeout=60s`, `ShutdownTimeout=10s`.

```go
type Config struct {
	Environment string
	DatabaseURL string
	HTTP        HTTP
	OTEL        OTEL
}

var ErrDatabaseURLRequired = errors.New("DATABASE_URL is required")

func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" { return Config{}, ErrDatabaseURLRequired }
	return Config{
		Environment: envOr("APP_ENV", "development"),
		DatabaseURL: databaseURL,
		HTTP: HTTP{Addr: envOr("HTTP_ADDR", ":8080"), ReadHeaderTimeout: 5*time.Second,
			ReadTimeout: 15*time.Second, WriteTimeout: 30*time.Second,
			IdleTimeout: 60*time.Second, ShutdownTimeout: 10*time.Second},
		OTEL: OTEL{Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"), ServiceName: envOr("OTEL_SERVICE_NAME", "blob-api")},
	}, nil
}
```

- [ ] **Step 3: Write failing request-id middleware tests**

Test that an invalid inbound id is replaced and a valid 32-hex id is preserved:

```go
func TestMiddlewareReplacesUntrustedRequestID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "../../secret")
	rec := httptest.NewRecorder()
	Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, FromContext(r.Context()))
	})).ServeHTTP(rec, req)
	id := rec.Header().Get("X-Request-ID")
	if ok, _ := regexp.MatchString(`^[a-f0-9]{32}$`, id); !ok { t.Fatalf("unsafe id %q", id) }
}
```

Run and expect FAIL:

```powershell
go -C server test ./internal/platform/requestid -v
```

- [ ] **Step 4: Implement request IDs with the standard library**

Generate 16 random bytes with `crypto/rand`, hex-encode them, store them in context and response header. Accept inbound ids only when they match `^[a-f0-9]{32}$`.

- [ ] **Step 5: Write failing liveness handler tests**

```go
func TestLiveness(t *testing.T) {
	h := NewHandler(system.NewHealthChecker(nil))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/live", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("got %d", rec.Code) }
	if rec.Header().Get("Content-Type") != "application/json" { t.Fatal("missing JSON content type") }
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` { t.Fatalf("got %s", rec.Body.String()) }
}
```

Run:

```powershell
go -C server test ./internal/transport/httpapi -run TestLiveness -v
```

Expected: FAIL because `NewHandler` is undefined.

- [ ] **Step 6: Implement system health and strict handlers**

```go
type Pinger interface { Ping(context.Context) error }

type HealthChecker struct { db Pinger }

func (h HealthChecker) Live(context.Context) error { return nil }
func (h HealthChecker) Ready(ctx context.Context) error {
	if h.db == nil { return errors.New("database unavailable") }
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return h.db.Ping(ctx)
}
```

`GetLiveness` returns typed `GetLiveness200JSONResponse`. `GetReadiness` returns typed 200 or safe 503 with code `dependency_unavailable`, message `Service is not ready`, and current request id. Never include the database error. Build the route handler with `NewStrictHandler` and `HandlerWithOptions(..., StdHTTPServerOptions{BaseURL: "/api/v1"})`; do not hand-prefix individual routes.

- [ ] **Step 7: Compose and run the API process**

`httpserver.Server` must set every timeout from config, run `ListenAndServe`, treat `http.ErrServerClosed` as normal, and call `Shutdown` with a fresh timeout context. `cmd/api/main.go` creates a signal context for `os.Interrupt` and `syscall.SIGTERM`, calls `app.RunAPI`, and exits non-zero on error.

Run:

```powershell
go -C server test ./...
go -C server vet ./...
go -C server build ./cmd/api
```

Expected: all commands exit 0.

- [ ] **Step 8: Commit**

```powershell
git add server
git commit -m "feat: add Go API liveness service"
```

---

### Task 4: Connect PostgreSQL, migrations and readiness

**Files:**
- Create: `server/migrations/00001_system.sql`
- Create: `server/internal/platform/database/postgres.go`
- Test: `server/internal/platform/database/postgres_integration_test.go`
- Modify: `server/internal/app/api.go`
- Create: `server/internal/testdb/testdb.go`

**Interfaces:**
- Consumes: `config.Config.DatabaseURL`; `system.Pinger`.
- Produces: `database.Open(context.Context, string) (*pgxpool.Pool, error)`; migrated PostgreSQL; readiness backed by a real `Ping`.

- [ ] **Step 1: Add pgx and the first reversible migration**

Run:

```powershell
go -C server get github.com/jackc/pgx/v5@v5.10.0
```

Create `server/migrations/00001_system.sql`:

```sql
-- +goose Up
CREATE TABLE jobs (
    id uuid PRIMARY KEY,
    kind text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL CHECK (status IN ('pending', 'running', 'done', 'failed')),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at timestamptz NOT NULL DEFAULT now(),
    locked_at timestamptz,
    last_error_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX jobs_claim_idx ON jobs (available_at, created_at) WHERE status = 'pending';
CREATE INDEX jobs_reclaim_idx ON jobs (locked_at) WHERE status = 'running';
-- +goose Down
DROP TABLE jobs;
```

- [ ] **Step 2: Start only PostgreSQL and run migrations**

Before full Compose exists, run a disposable explicit container:

```powershell
docker run --name blob-plan-postgres -e POSTGRES_USER=blob -e POSTGRES_PASSWORD=blob -e POSTGRES_DB=blob -p 55432:5432 -d postgres:17.11-alpine
$env:DATABASE_URL='postgres://blob:blob@127.0.0.1:55432/blob?sslmode=disable'
go -C server tool goose -dir migrations postgres $env:DATABASE_URL up
go -C server tool goose -dir migrations postgres $env:DATABASE_URL status
```

Expected: migration `00001` is applied. This named container is removed in Step 7 after verifying its exact name.

- [ ] **Step 3: Write the failing real-PostgreSQL test**

```go
func TestOpenAndPing(t *testing.T) {
	url := testdb.URL(t)
	pool, err := Open(context.Background(), url)
	if err != nil { t.Fatal(err) }
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil { t.Fatal(err) }
}
```

`testdb.URL(t)` reads `TEST_DATABASE_URL`; it calls `t.Skip` only outside CI and `t.Fatal` when `CI=true` without a URL.

Run:

```powershell
$env:TEST_DATABASE_URL=$env:DATABASE_URL
go -C server test ./internal/platform/database -run TestOpenAndPing -v
```

Expected: FAIL because `Open` is undefined.

- [ ] **Step 4: Implement the bounded pool**

Parse with `pgxpool.ParseConfig`, then set `MaxConns=20`, `MinConns=0`, `MaxConnLifetime=30m`, `MaxConnIdleTime=5m`, `HealthCheckPeriod=30s`. Connect with a 5-second context and close on initial ping failure.

- [ ] **Step 5: Wire readiness to PostgreSQL**

Modify `app.RunAPI` to open the pool before the HTTP server, `defer pool.Close()`, and inject it into `system.NewHealthChecker(pool)`.

Run API and verify:

```powershell
$env:DATABASE_URL='postgres://blob:blob@127.0.0.1:55432/blob?sslmode=disable'
$process = Start-Process go -ArgumentList '-C','server','run','./cmd/api' -PassThru -WindowStyle Hidden
try { Start-Sleep -Seconds 2; Invoke-RestMethod http://127.0.0.1:8080/api/v1/health/ready } finally { Stop-Process -Id $process.Id }
```

Expected JSON: `{"status":"ok"}`.

- [ ] **Step 6: Verify migration rollback on a separate test database**

```powershell
docker exec blob-plan-postgres createdb -U blob blob_migration_test
$migrationUrl='postgres://blob:blob@127.0.0.1:55432/blob_migration_test?sslmode=disable'
go -C server tool goose -dir migrations postgres $migrationUrl up
go -C server tool goose -dir migrations postgres $migrationUrl down
go -C server tool goose -dir migrations postgres $migrationUrl up
```

Expected: all three commands succeed.

- [ ] **Step 7: Remove the exact temporary container**

```powershell
$container = docker inspect --format '{{.Name}}' blob-plan-postgres
if ($container -ne '/blob-plan-postgres') { throw "Unexpected container: $container" }
docker rm -f blob-plan-postgres
```

- [ ] **Step 8: Commit**

```powershell
git add server
git commit -m "feat: add PostgreSQL foundation"
```

---

### Task 5: Add sqlc and an at-least-once PostgreSQL worker

**Files:**
- Create: `server/sqlc.yaml`
- Create: `server/queries/system.sql`
- Generate: `server/internal/db/*.go`
- Create: `server/internal/modules/system/jobs.go`
- Test: `server/internal/modules/system/jobs_integration_test.go`
- Create: `server/internal/app/worker.go`
- Create: `server/cmd/worker/main.go`

**Interfaces:**
- Consumes: `pgxpool.Pool`; migrations from Task 4.
- Produces: generated `db.DBTX` and `db.Queries` methods; `Jobs.RunOnce(context.Context) (bool, error)`; `app.RunWorker(context.Context) error`.

- [ ] **Step 1: Configure sqlc and write exact queries**

Create `server/sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: migrations
    queries: queries
    gen:
      go:
        package: db
        out: internal/db
        sql_package: pgx/v5
        emit_interface: true
        emit_json_tags: true
```

Create `server/queries/system.sql` with named queries:

```sql
-- name: EnqueueJob :one
INSERT INTO jobs (id, kind, payload, status, available_at)
VALUES ($1, $2, $3, 'pending', $4)
RETURNING *;

-- name: ClaimJob :one
WITH candidate AS (
    SELECT id FROM jobs
    WHERE (status = 'pending' AND available_at <= now())
       OR (status = 'running' AND locked_at < now() - interval '5 minutes')
    ORDER BY available_at, created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE jobs
SET status = 'running', attempts = attempts + 1, locked_at = now(), updated_at = now()
WHERE id = (SELECT id FROM candidate)
RETURNING *;

-- name: CompleteJob :exec
UPDATE jobs SET status = 'done', locked_at = NULL, last_error_code = NULL, updated_at = now()
WHERE id = $1 AND status = 'running';

-- name: FailJob :exec
UPDATE jobs
SET status = CASE WHEN attempts >= 5 THEN 'failed' ELSE 'pending' END,
    available_at = CASE WHEN attempts >= 5 THEN available_at ELSE now() + make_interval(secs => LEAST(300, attempts * attempts)) END,
    locked_at = NULL,
    last_error_code = $2,
    updated_at = now()
WHERE id = $1 AND status = 'running';
```

Run:

```powershell
go -C server tool sqlc generate
```

- [ ] **Step 2: Write failing claim-concurrency integration test**

Enqueue one `system.noop` job through generated `db.Queries.EnqueueJob`, call `RunOnce` concurrently twice, and assert the handler executes exactly once. Use a real migrated `TEST_DATABASE_URL` and truncate `jobs` before the test.

Run:

```powershell
go -C server test ./internal/modules/system -run TestOnlyOneWorkerClaimsJob -v
```

Expected: FAIL because `Jobs.RunOnce` is undefined.

- [ ] **Step 3: Implement the minimal worker service**

```go
type Handler func(context.Context, json.RawMessage) error

type Jobs struct {
	queries *db.Queries
	handlers map[string]Handler
}

func (j *Jobs) RunOnce(ctx context.Context) (bool, error) {
	job, err := j.queries.ClaimJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) { return false, nil }
	if err != nil { return false, err }
	handler, ok := j.handlers[job.Kind]
	if !ok { return true, j.fail(ctx, job, "unknown_job_kind") }
	if err := handler(ctx, job.Payload); err != nil { return true, j.fail(ctx, job, "handler_failed") }
	return true, j.queries.CompleteJob(ctx, job.ID)
}
```

`fail` accepts only the two stable codes shown above; it never persists `err.Error()` or payload data. Register only `system.noop`, whose handler returns `nil`. Poll every second only when no job was claimed; never busy-loop. Stop immediately when context is cancelled. Add `// ponytail: fixed 5-minute lease; add heartbeat before introducing handlers that can legitimately run longer` above the claim method. Delivery is at-least-once, so every registered handler must be idempotent.

- [ ] **Step 4: Verify success, retry and terminal failure**

Add integration tests asserting:

- two concurrent workers cannot claim the same job;
- a successful handler produces `done`;
- a failing handler returns to `pending` with `last_error_code=handler_failed` for attempts 1–4;
- attempt 5 produces `failed`;
- a `running` job locked six minutes ago is reclaimed after a simulated worker crash;
- cancellation exits without claiming more work.

Between retry assertions, set only that test job's `available_at=now()` through SQL; do not sleep in the test.

Run:

```powershell
go -C server test ./internal/modules/system -v
```

Expected: PASS.

- [ ] **Step 5: Create and build the worker process**

`cmd/worker/main.go` mirrors API signal handling. `app.RunWorker` loads config, opens PostgreSQL, constructs generated queries and calls the polling loop. No HTTP server and no Redis.

```powershell
go -C server test ./...
go -C server vet ./...
go -C server build ./cmd/api ./cmd/worker
```

Expected: exit 0.

- [ ] **Step 6: Commit**

```powershell
git add server
git commit -m "feat: add PostgreSQL job worker"
```

---

### Task 6: Create the Nuxt shell, localization and generated client

**Files:**
- Create: `web/package.json`
- Create: `web/nuxt.config.ts`
- Create: `web/openapi-ts.config.ts`
- Create: `web/app/app.vue`
- Create: `web/app/pages/index.vue`
- Create: `web/app/assets/css/main.css`
- Create: `web/app/composables/useBlobApi.ts`
- Create: `web/i18n/locales/ru.json`
- Create: `web/i18n/locales/en.json`
- Create: `web/i18n/locales/ar.json`
- Generate: `web/app/api/generated/*`
- Test: `web/test/status.test.ts`

**Interfaces:**
- Consumes: `/api/v1/health/ready`; OpenAPI `HealthResponse`.
- Produces: localized SSR page and `useBlobApi<T>(path)` using internal base during SSR and same-origin base in browser.

- [ ] **Step 1: Create the pinned Nuxt package**

Create `web/package.json`:

```json
{
  "name": "@blob/web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "nuxt dev",
    "build": "nuxt build",
    "check": "nuxt typecheck",
    "test:run": "vitest run",
    "generate:api": "openapi-ts"
  },
  "dependencies": {
    "@nuxtjs/i18n": "10.6.0",
    "nuxt": "4.5.2",
    "vue": "3.5.42"
  },
  "devDependencies": {
    "@hey-api/openapi-ts": "0.99.0",
    "@nuxt/test-utils": "4.3.2",
    "@vue/test-utils": "2.4.6",
    "happy-dom": "20.14.5",
    "typescript": "5.9.2",
    "vitest": "3.2.4",
    "vue-tsc": "3.3.11"
  }
}
```

If pnpm reports that an exact transitive compatibility pair has been withdrawn, stop and update this plan/spec with the nearest non-withdrawn patch; do not silently use a range.

Run:

```powershell
corepack enable
corepack prepare pnpm@11.26.0 --activate
pnpm install
```

- [ ] **Step 2: Configure Nuxt, i18n and runtime bases**

```ts
export default defineNuxtConfig({
  compatibilityDate: '2026-09-14',
  css: ['~/assets/css/main.css'],
  modules: ['@nuxtjs/i18n'],
  runtimeConfig: {
    apiInternalBase: process.env.API_INTERNAL_BASE ?? 'http://127.0.0.1:8080/api/v1',
    public: { apiBase: process.env.NUXT_PUBLIC_API_BASE ?? '/api/v1' },
  },
  i18n: {
    defaultLocale: 'ru',
    strategy: 'prefix_except_default',
    locales: [
      { code: 'ru', language: 'ru-RU', file: 'ru.json', dir: 'ltr' },
      { code: 'en', language: 'en-US', file: 'en.json', dir: 'ltr' },
      { code: 'ar', language: 'ar', file: 'ar.json', dir: 'rtl' },
    ],
  },
})
```

Each locale defines `appName`, `statusTitle`, `statusReady`, `statusUnavailable`. `app.vue` sets `lang` and `dir` from the active locale.

- [ ] **Step 3: Configure and generate the TypeScript client**

Create `web/openapi-ts.config.ts`:

```ts
import { defineConfig } from '@hey-api/openapi-ts'

export default defineConfig({
  input: '../api/openapi.yaml',
  output: 'app/api/generated',
  plugins: ['@hey-api/client-fetch', '@hey-api/typescript', '@hey-api/sdk'],
})
```

`@hey-api/client-fetch` in this list names the generator's bundled plugin; do not install the deprecated standalone package.

Run:

```powershell
pnpm --dir web generate:api
```

Commit the output. Do not edit it.

- [ ] **Step 4: Write the failing SSR status test**

```ts
import { mountSuspended, mockNuxtImport } from '@nuxt/test-utils/runtime'
import { describe, expect, it, vi } from 'vitest'
import IndexPage from '../app/pages/index.vue'

mockNuxtImport('$fetch', () => vi.fn().mockResolvedValue({ status: 'ok' }))

describe('status page', () => {
  it('renders the API readiness result', async () => {
    const wrapper = await mountSuspended(IndexPage)
    expect(wrapper.get('[data-test="api-status"]').text()).toContain('Готов')
  })
})
```

Run:

```powershell
pnpm --dir web test:run
```

Expected: FAIL because the page does not exist.

- [ ] **Step 5: Implement the minimal accessible page**

`useBlobApi<T>` chooses `apiInternalBase` on server and `public.apiBase` in browser, then calls `$fetch<T>` with no credentials in URL. `index.vue` uses `useAsyncData`, renders one `<main>`, one `<h1>`, and a status element with `role="status"`. It shows a safe localized unavailable state without stack details.

On the server, `useBlobApi` delegates to the traced `blobApiFetch` added in Task 7; in the browser it calls the same-origin generated client. There is no second HTTP-client abstraction.

Use CSS logical properties (`margin-inline`, `padding-inline`) and:

```css
:root { color-scheme: light dark; }
body {
  margin: 0;
  background: light-dark(#f6f7fb, #0d1117);
  color: light-dark(#172033, #eef2f8);
  font-family: system-ui, sans-serif;
}
```

- [ ] **Step 6: Verify all locales, RTL and build**

Add tests for `/`, `/en`, `/ar` asserting `lang`, `dir`, localized heading and status. Run:

```powershell
pnpm --dir web test:run
pnpm --dir web check
pnpm --dir web build
```

Expected: all exit 0.

- [ ] **Step 7: Commit**

```powershell
git add web pnpm-lock.yaml
git commit -m "feat: add Nuxt application shell"
```

---

### Task 7: Add safe logs and distributed tracing

**Files:**
- Create: `server/internal/platform/observability/otel.go`
- Test: `server/internal/platform/observability/otel_test.go`
- Create: `server/internal/platform/observability/http.go`
- Modify: `server/internal/app/api.go`
- Modify: `server/internal/app/worker.go`
- Create: `web/server/plugins/otel.ts`
- Create: `web/server/utils/blob-api.ts`
- Test: `web/test/trace-context.test.ts`

**Interfaces:**
- Consumes: OTLP endpoint and service name; request-id middleware; Nuxt internal API base.
- Produces: W3C `traceparent` propagation Nuxt SSR → Go; API/job/DB spans; JSON allowlist logs.

- [ ] **Step 1: Add pinned OpenTelemetry dependencies**

```powershell
go -C server get go.opentelemetry.io/otel@v1.39.0
go -C server get go.opentelemetry.io/otel/sdk@v1.39.0
go -C server get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.39.0
go -C server get go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.64.0
pnpm --dir web add @opentelemetry/api@1.9.0 @opentelemetry/sdk-node@0.203.0 @opentelemetry/exporter-trace-otlp-grpc@0.203.0
```

- [ ] **Step 2: Write the failing Go redaction/allowlist test**

Send a request containing `Cookie`, `Authorization`, query `token=secret`, and body `top-secret`. Capture JSON log output and assert none of those values occur; assert `method`, route template, status, duration, request id and trace id do occur.

Run:

```powershell
go -C server test ./internal/platform/observability -run TestHTTPLogAllowlist -v
```

Expected: FAIL because the middleware is undefined.

- [ ] **Step 3: Implement Go tracing and logging**

`Setup(ctx, serviceName, endpoint)` returns `shutdown func(context.Context) error`. Empty endpoint installs a local SDK with `NeverSample`; non-empty endpoint uses OTLP gRPC and `AlwaysSample` for Stage 1. Install `TraceContext` + `Baggage` propagation, but never copy baggage values into logs.

Wrap the mux in this order:

```text
request id → otelhttp → access log → strict OpenAPI handler
```

Access logs contain only: `timestamp`, `level`, `service`, `request_id`, `trace_id`, `method`, matched route template, `status`, `duration_ms`.

Create manual spans `postgres.ping` and `job.run`; attributes are limited to operation name, job kind and success/failure. HTTP trace context reaches PostgreSQL spans in that request. A worker job starts its own root span because Stage 1A does not persist producer trace context. Never record SQL, payload or error details containing user data.

- [ ] **Step 4: Write and implement Nuxt trace propagation test**

Test `blobApiFetch` against a local test server that records headers. Start an active span, call the helper, and assert `traceparent` matches W3C format. Also assert no cookie or URL query is added by the helper.

`web/server/plugins/otel.ts` starts one NodeSDK instance only when `OTEL_EXPORTER_OTLP_ENDPOINT` is non-empty. `blobApiFetch` creates a child span, injects context into a new `Headers`, and calls `$fetch` using `apiInternalBase`.

- [ ] **Step 5: Verify telemetry shutdown and tests**

Both API and worker defer the returned shutdown with a fresh 5-second context. Run:

```powershell
go -C server test ./...
pnpm --dir web test:run
pnpm --dir web check
```

Expected: exit 0; secret canary values absent from captured telemetry.

- [ ] **Step 6: Commit**

```powershell
git add server web pnpm-lock.yaml
git commit -m "feat: add safe distributed tracing"
```

---

### Task 8: Containerize the vertical slice behind one origin

**Files:**
- Create: `server/Dockerfile`
- Create: `web/Dockerfile`
- Create: `compose.yaml`
- Create: `Caddyfile`
- Create: `server/internal/platform/httpserver/security.go`
- Test: `server/internal/platform/httpserver/security_test.go`

**Interfaces:**
- Consumes: API on `:8080`, Nuxt on `:3000`, PostgreSQL.
- Produces: `http://127.0.0.1:8088`; `/api/v1/*` routed to Go, other paths to Nuxt; non-root app images.

- [ ] **Step 1: Write failing baseline header tests**

Assert API responses include `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, and no `Server` header. Do not add CSP to JSON API; CSP belongs on Nuxt HTML in a later UI-security task.

Run and expect FAIL:

```powershell
go -C server test ./internal/platform/httpserver -run TestSecurityHeaders -v
```

- [ ] **Step 2: Implement the small security middleware**

Set only the two headers above, then call next. Do not add a dependency or a speculative configurable policy object.

- [ ] **Step 3: Create multi-stage non-root Dockerfiles**

`server/Dockerfile` has two application targets. It builds both binaries from `golang:1.27.1-alpine`, then copies one selected binary into `gcr.io/distroless/static-debian12:nonroot`; use build arg `TARGET=api|worker` and command `/app`. A separate `migration-tools` target stays on the Go builder image, creates an unprivileged user, copies `go.mod`, `go.sum` and `migrations/`, and has entrypoint `go tool goose -dir /src/migrations`. It contains no application server and is used only by the one-shot Compose migration service.

`web/Dockerfile` builds with `node:24.21.0-alpine`, activates pnpm 11.26.0 through Corepack, runs `pnpm install --frozen-lockfile` and `pnpm build`, then copies `.output` into a clean Node image and runs as `node`.

- [ ] **Step 4: Create the one-origin routing**

Create `Caddyfile`:

```caddyfile
:8088 {
  @api path /api/*
  handle @api {
    reverse_proxy api:8080
  }
  handle {
    reverse_proxy web:3000
  }
}
```

Create `compose.yaml` with exact services:

- `postgres`: `postgres:17.11-alpine`, named volume, healthcheck `pg_isready`;
- `migrate`: `server/Dockerfile` target `migration-tools`, command `postgres $DATABASE_URL up`, depends on healthy postgres, exits 0;
- `api`: depends on successful migrate, exposes only to internal network;
- `worker`: depends on successful migrate, no public port;
- `web`: internal port 3000;
- `proxy`: `caddy:2.10.2-alpine`, the only service publishing `127.0.0.1:8088:8088`.

The four version tags above are fixed to product patch versions; immutable image digests and container scanning are part of the pre-release hardening in plan 1G.

Set `read_only: true`, `tmpfs: /tmp`, `cap_drop: [ALL]`, `security_opt: [no-new-privileges:true]` for API, worker and web. Add CPU/memory limits supported by Compose. PostgreSQL alone gets the named persistent volume.

- [ ] **Step 5: Validate and run the stack**

```powershell
docker compose config --quiet
docker compose up --build -d
try {
  Invoke-RestMethod http://127.0.0.1:8088/api/v1/health/live
  Invoke-RestMethod http://127.0.0.1:8088/api/v1/health/ready
  $page = Invoke-WebRequest http://127.0.0.1:8088/
  if ($page.StatusCode -ne 200 -or $page.Content -notmatch 'Blob') { throw 'Web smoke failed' }
} finally {
  docker compose down
}
```

Expected: both health endpoints return `status=ok`; page is 200; only port 8088 is published.

- [ ] **Step 6: Commit**

```powershell
git add server web compose.yaml Caddyfile
git commit -m "feat: containerize Blob foundation"
```

---

### Task 9: Add generated-code checks and pinned CI

**Files:**
- Create: `scripts/check-generated.ps1`
- Create: `scripts/check-actions-pinned.ps1`
- Create: `.github/workflows/ci.yml`
- Modify: `package.json`

**Interfaces:**
- Consumes: root generation/test/build commands.
- Produces: reproducible CI gate with PostgreSQL 17.11 and full-SHA Actions.

- [ ] **Step 1: Write the failing generated-code drift script**

Create `scripts/check-generated.ps1`:

```powershell
$ErrorActionPreference = 'Stop'
pnpm generate
$dirty = git status --porcelain -- api/openapi.yaml server/internal/transport/httpapi server/internal/db web/app/api/generated
if ($dirty) {
  Write-Error "Generated code is stale:`n$dirty"
}
```

Temporarily append a harmless newline to a generated file, run the script and confirm it exits non-zero. Restore only that verified file with `git restore -- <exact-file>`.

- [ ] **Step 2: Write the action pin checker**

Create `scripts/check-actions-pinned.ps1`:

```powershell
$ErrorActionPreference = 'Stop'
$bad = Select-String -Path '.github/workflows/*.yml' -Pattern 'uses:\s+[^\s]+@(?![0-9a-f]{40}(?:\s|$))'
if ($bad) {
  $bad | ForEach-Object { Write-Error $_.Line }
}
```

- [ ] **Step 3: Create CI with pinned actions**

Use:

```yaml
name: ci
on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  verify:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:17.11-alpine
        env:
          POSTGRES_USER: blob
          POSTGRES_PASSWORD: blob
          POSTGRES_DB: blob_test
        ports: ["5432:5432"]
        options: >-
          --health-cmd "pg_isready -U blob -d blob_test"
          --health-interval 5s --health-timeout 5s --health-retries 10
    env:
      CI: "true"
      DATABASE_URL: postgres://blob:blob@127.0.0.1:5432/blob_test?sslmode=disable
      TEST_DATABASE_URL: postgres://blob:blob@127.0.0.1:5432/blob_test?sslmode=disable
    steps:
      - uses: actions/checkout@1af3b93b6815bc44a9784bd300feb67ff0d1eeb3 # v6.0.0
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
        with:
          go-version-file: .go-version
          cache-dependency-path: server/go.sum
      - uses: actions/setup-node@2028fbc5c25fe9cf00d9f06a71cc4710d4507903 # v6.0.0
        with:
          node-version-file: .node-version
      - run: corepack enable
      - run: corepack prepare pnpm@11.26.0 --activate
      - run: pnpm install --frozen-lockfile
      - run: go -C server tool goose -dir migrations postgres "$DATABASE_URL" up
      - run: pwsh ./scripts/check-generated.ps1
      - run: pwsh ./scripts/check-actions-pinned.ps1
      - run: pnpm check
      - run: pnpm test
      - run: go -C server build ./cmd/api ./cmd/worker
      - run: pnpm --dir web build
      - run: docker compose config --quiet
```

- [ ] **Step 4: Add root scripts and verify locally**

Add `check:generated` and `check:actions` to root `package.json`, then run:

```powershell
pnpm install --frozen-lockfile
pnpm generate
pnpm check
pnpm test
pwsh ./scripts/check-generated.ps1
pwsh ./scripts/check-actions-pinned.ps1
docker compose config --quiet
```

Expected: all exit 0 and `git status --porcelain` remains empty after generation.

- [ ] **Step 5: Commit**

```powershell
git add .github scripts package.json pnpm-lock.yaml
git commit -m "ci: verify Blob platform foundation"
```

---

### Task 10: Prove the Stage 1A vertical slice and document evidence

**Files:**
- Create: `docs/verification/stage-1a.md`
- Modify: `README.md`

**Interfaces:**
- Consumes: all Stage 1A deliverables.
- Produces: reproducible evidence that web, API, PostgreSQL and worker function together; handoff contract for plan 1B.

- [ ] **Step 1: Run the clean verification suite**

```powershell
pnpm install --frozen-lockfile
pnpm generate
pnpm check
pnpm test
go -C server vet ./...
go -C server build ./cmd/api ./cmd/worker
docker compose config --quiet
docker compose up --build -d
```

Expected: every command exits 0; Compose services become healthy.

- [ ] **Step 2: Exercise the public origin and dependency failure**

```powershell
$live = Invoke-RestMethod http://127.0.0.1:8088/api/v1/health/live
$ready = Invoke-RestMethod http://127.0.0.1:8088/api/v1/health/ready
$web = Invoke-WebRequest http://127.0.0.1:8088/
if ($live.status -ne 'ok' -or $ready.status -ne 'ok' -or $web.StatusCode -ne 200) { throw 'Healthy smoke failed' }

docker compose stop postgres
$stillLive = Invoke-WebRequest http://127.0.0.1:8088/api/v1/health/live
try {
  Invoke-WebRequest http://127.0.0.1:8088/api/v1/health/ready -ErrorAction Stop
  throw 'Readiness unexpectedly succeeded'
} catch {
  if ($_.Exception.Response.StatusCode.value__ -ne 503) { throw }
}
if ($stillLive.StatusCode -ne 200) { throw 'Liveness failed with dependency down' }
```

Expected: liveness stays 200; readiness becomes 503 with safe `dependency_unavailable` and no connection details.

- [ ] **Step 3: Verify worker semantics once**

Restart PostgreSQL, wait for health, insert one `system.noop` job using a small SQL command with a fixed UUID, then poll until status is `done`. Assert `attempts=1`. Do not inspect or log payload contents.

- [ ] **Step 4: Record evidence**

Create `docs/verification/stage-1a.md` containing:

```markdown
# Stage 1A verification

- Date and commit SHA
- Host OS and Docker version
- Go, Node, pnpm and PostgreSQL versions
- Commands executed and exit codes
- Healthy response status codes
- Dependency-down liveness/readiness result
- Worker job final status and attempt count
- Known limitation: no production PaaS deployment until plan 1G
```

Include no secrets, full environment dumps, connection strings with passwords, cookies or request bodies.

- [ ] **Step 5: Stop the stack without deleting the database volume**

```powershell
docker compose down
```

Do not use `--volumes`; the local PostgreSQL volume is intentionally recoverable.

- [ ] **Step 6: Commit**

```powershell
git add README.md docs/verification/stage-1a.md
git commit -m "docs: record Stage 1A verification"
```

---

## Stage 1A acceptance checklist

- [ ] New implementation exists only under `web/`, `server/` and `api/`; old attempts are untouched and ignored.
- [ ] `/api/v1/health/live` is independent of PostgreSQL; `/ready` reflects PostgreSQL.
- [ ] OpenAPI generates strict Go server types and a TypeScript client with no drift.
- [ ] Integration tests use real PostgreSQL.
- [ ] Worker claims jobs with `FOR UPDATE SKIP LOCKED`, bounded retries and no duplicate claim.
- [ ] Nuxt SSR renders Russian, English and Arabic/RTL status pages.
- [ ] Browser and SSR API access use a single public origin.
- [ ] API, worker and web run in bounded non-root containers.
- [ ] Request IDs are sanitized; logs and traces exclude secret canaries.
- [ ] W3C trace context crosses Nuxt SSR → Go → PostgreSQL spans; worker jobs emit independent root spans without payload data.
- [ ] CI actions use full immutable SHAs and the workflow passes from a clean checkout.
- [ ] Dependency-down behavior, migration rollback and worker semantics are recorded.

## Handoff to plan 1B

Plan 1B may rely only on these stable interfaces:

- `httpapi.StrictServerInterface` and the root `/api/v1` mux;
- `db.DBTX`, generated `db.Queries`, `pgxpool.Pool` and transaction-compatible sqlc methods;
- `requestid.FromContext(ctx)`;
- OpenTelemetry provider and safe HTTP middleware;
- generated `db.Queries.EnqueueJob`, `Jobs.RunOnce` and registered job handlers;
- generated TypeScript API client;
- `useBlobApi` SSR/browser base selection;
- Compose PostgreSQL and the pinned CI toolchain.

It must not add Redis, an ORM, a generic repository, another Go HTTP framework, another source of API types or business logic in Nuxt.
