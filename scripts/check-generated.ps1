$ErrorActionPreference = 'Stop'
pnpm generate
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
$dirty = git status --porcelain -- api/openapi.yaml server/internal/transport/httpapi server/internal/db web/app/api/generated
if ($dirty) {
  Write-Error "Generated code is stale:`n$dirty"
}
