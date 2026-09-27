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

See [Stage 1A verification](docs/verification/stage-1a.md) for the full-stack smoke results and host-specific command substitutions.
