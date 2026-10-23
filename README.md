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

Copy `.env.example` to `.env` (preserve an existing `.env` rather than
overwrite it), then generate a local browser signing key before starting
Compose:

```powershell
$blobKeyBytes = New-Object byte[] 32
$blobRng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try { $blobRng.GetBytes($blobKeyBytes) } finally { $blobRng.Dispose() }
$env:AUTH_BROWSER_KEY = [Convert]::ToBase64String($blobKeyBytes)
$env:AUTH_PUBLIC_ORIGIN = 'https://localhost:8443'
```

Then run `docker compose up --build`. Open `http://127.0.0.1:8088`.

`AUTH_PUBLIC_ORIGIN`/`AUTH_BROWSER_KEY` configure the API's session
cookie/CSRF/CORS trust boundary; the worker never reads this key. No secret
is shipped in `.env.example` or `compose.yaml`; a missing/malformed key
fails API startup safely rather than at `docker compose config`. This
increment issues no session over HTTP yet (no login/registration route
exists); the HTTPS origin above identifies the upcoming trusted browser
origin for the next increment, not an already-configured HTTPS listener.
Persist the key through real deployment secret configuration once sessions
are user-facing.

## Verification

- `pnpm generate`
- `pnpm check`
- `pnpm test`
- `docker compose config --quiet`

See [Stage 1A verification](docs/verification/stage-1a.md) for the full-stack smoke results and host-specific command substitutions.
