# linkedin-cron

A lightweight Go monolith to draft, schedule, and publish social posts (LinkedIn + Facebook Pages) with:

- server-rendered HTML UI (`net/http` + `html/template` + HTMX)
- minimal authenticated JSON API for agent use
- GUI-managed API keys for agent authentication
- SQLite storage with handwritten SQL
- one-shot scheduler command meant for `systemd.timer`
- pluggable publisher (default: dry-run)

## Architecture

- `cmd/server`: long-running HTTP server for UI + API
- `cmd/scheduler`: one-shot runner, intended every minute from systemd
- `internal/db`: SQLite open/migrate + store methods
- `internal/scheduler`: due-selection + retry bookkeeping + send-now flow
- `internal/publisher`: publisher interface + dry-run implementation
- `internal/linkedin`: LinkedIn HTTP publisher (config-gated)
- `internal/facebook`: Facebook Page Graph API publisher (config-gated)
- `channels` and `post_channels` DB model to manage publish channels via GUI/API
- `publish_attempts` table for per-channel delivery history and retry state
- `channel_audit_events` table for channel credential/update audit trail

## Agent Docs

- Deployment: `docs/agents-deployment.md`
- API usage/security: `docs/agents-usage.md`
- Fase-1 backlog: `docs/phase1-backlog.md`

There is no internal background job loop in the server and no Node build pipeline.

## Prerequisites

- Go 1.22+ (tested with Go 1.24)
- Linux with systemd for production timer/service setup

`sqlite3` CLI is optional; the app uses SQLite directly from Go.

## Local Setup

```bash
cp .env.example .env
make run
```

Default URL: `http://localhost:8080`

Default Basic Auth credentials from `.env.example`:

- user: `admin`
- password: `admin`

SQLite location default: `./data/linkedin-cron.db`

## Make Targets

- `make build` – builds `bin/linkedin-cron-server` and `bin/linkedin-cron-scheduler`
- `make run` – starts HTTP server
- `make run-scheduler` – executes one scheduler run
- `make test` – runs `go test ./...`
- `make fmt` – runs `go fmt ./...`
- `make lint` – runs `golangci-lint` if installed
- `make clean` – removes `bin/`
- `make docker-build` – builds local Docker image
- `make docker-up` – starts docker-compose stack
- `make docker-down` – stops docker-compose stack

## Configuration

Environment variables:

- `APP_ADDR` (default `:8080`)
- `APP_ENV`
- `APP_BASE_URL`
- `APP_DB_PATH` (default `./data/linkedin-cron.db`)
- `APP_BASIC_AUTH_USER`
- `APP_BASIC_AUTH_PASS`
- `APP_STATIC_API_KEYS` (optional bootstrap API keys, format: `name:token,name2:token2`)
- `APP_SESSION_SECURE`
- `APP_TIMEZONE` (default `UTC`)
- `PUBLISHER_MODE` (`dry-run`, `linkedin`, or `facebook-page`, default `dry-run`)
- `LINKEDIN_ACCESS_TOKEN`
- `LINKEDIN_AUTHOR_URN`
- `LINKEDIN_API_BASE_URL` (default `https://api.linkedin.com`)
- `FACEBOOK_PAGE_ACCESS_TOKEN`
- `FACEBOOK_PAGE_ID`
- `FACEBOOK_API_BASE_URL` (default `https://graph.facebook.com/v22.0`)

If `PUBLISHER_MODE=linkedin` or `PUBLISHER_MODE=facebook-page` but required credentials are missing, the app falls back to dry-run mode.

## API Authentication for Agents

API routes support:

- Basic Auth, or
- API key (`X-API-Key` or `Authorization: Bearer ...`)

API keys are created/revoked in `/settings`.

UI routes now support a friendly username/password login form (`/login`) backed by HttpOnly session cookies.

For non-technical onboarding, `/settings` includes **Give this to your bot** (auto key + copyable agent instructions).

For container-first/public deployments you can also bootstrap fixed API keys from env:

- `APP_STATIC_API_KEYS=bot-main:lcak_prod_xxx`

These env keys are accepted immediately by API auth middleware (in addition to DB-backed keys).

Security behavior:

- API keys are generated once and shown once.
- Only a hash is stored in SQLite (`api_keys.key_hash`).
- Revoked keys are immediately blocked.

## UI Endpoints

- `GET /healthz`
- `GET /login`
- `POST /login`
- `GET /logout`
- `POST /logout`
- `GET /calendar?view=month|week|list&date=YYYY-MM-DD`
- `GET /posts/new`
- `POST /posts`
- `GET /posts/{id}`
- `GET /posts/{id}/edit`
- `POST /posts/{id}`
- `POST /posts/{id}/delete`
- `POST /posts/{id}/send-now`
- `POST /posts/{id}/send-and-delete`
- `POST /posts/{id}/reschedule`
- `GET /posts/{id}/history`
- `GET /posts/bulk`
- `POST /posts/bulk/channels`
- `POST /posts/bulk/send-now`
- `GET /settings`
- `POST /settings/api-keys`
- `POST /settings/api-keys/bot-handoff`
- `POST /settings/api-keys/{id}/revoke`
- `GET /settings/channels`
- `POST /settings/channels`
- `GET /settings/channels/{id}/edit`
- `POST /settings/channels/{id}`
- `POST /settings/channels/{id}/test`
- `POST /settings/channels/{id}/disable`
- `POST /settings/channels/{id}/enable`
- `POST /settings/channels/{id}/delete`

Calendar UX highlights:

- month cards show compact labels (`LINKEDIN POST`, `FACEBOOK POST`, etc.) instead of full body text
- actions per card: `view post`, `edit post`, `send and delete`
- drag-and-drop rescheduling in month and week views (week includes vertical time grid)
- list mode combines ready dates + full post queue in one page

Bulk UI highlights:

- `/posts/bulk` supports lightweight server-side filters (`status`, `q`) for large queues.
- `/posts/bulk` keeps post/channel selections in browser `localStorage` (no backend session state).
- Bulk actions require an explicit confirmation checkbox and are also validated server-side.
- Partial failures return with failed post IDs preselected, so retries are one click.

## JSON API Endpoints

- `GET /api/v1/healthz`
- `GET /api/v1/posts`
- `GET /api/v1/posts/{id}`
- `POST /api/v1/posts`
- `PUT /api/v1/posts/{id}`
- `DELETE /api/v1/posts/{id}`
- `POST /api/v1/posts/{id}/send-now`
- `POST /api/v1/posts/{id}/send-and-delete`
- `POST /api/v1/posts/{id}/reschedule`
- `GET /api/v1/posts/{id}/attempts`
- `POST /api/v1/posts/bulk/send-now`
- `POST /api/v1/posts/bulk/channels`
- `GET /api/v1/settings/status`
- `POST /api/v1/settings/bot-handoff`
- `GET /api/v1/channels`
- `POST /api/v1/channels`
- `PUT /api/v1/channels/{id}`
- `GET /api/v1/channels/{id}/audit`
- `DELETE /api/v1/channels/{id}`
- `POST /api/v1/channels/{id}/test`
- `POST /api/v1/channels/{id}/disable`
- `POST /api/v1/channels/{id}/enable`

Notes:

- all API requests/responses are JSON
- timestamps are RFC3339
- API errors use `{ "error": "..." }`
- posts support `channel_ids` for assignment
- scheduled posts require at least one channel
- channel responses include explicit masked previews in `secret_preview` and presence booleans in `secret_presence`
- `GET /api/v1/posts/{id}/attempts` supports `status`, `channel_id`, `attempted_from`, `attempted_to`, plus pagination (`limit`, `offset`) and returns `{items, pagination}`
- `GET /api/v1/channels/{id}/audit` is paginated (`limit`, `offset`) and returns `{items, pagination}`
- `POST /api/v1/posts/{id}/reschedule` accepts `{ "scheduled_at": "RFC3339" }` and auto-moves draft/failed posts to `scheduled`
- `POST /api/v1/posts/{id}/send-and-delete` publishes immediately, then removes the post from the queue
- `POST /api/v1/settings/bot-handoff` creates an API key and returns copy-ready agent instructions

## Scheduler & Retry Behavior

Every run selects posts where:

- `status='scheduled'` and `scheduled_at <= now` with `next_retry_at IS NULL`
- or `next_retry_at <= now`

Batch size is 100 per run.

For posts with channel assignments, the scheduler executes each `(post, channel)` target independently and writes attempt rows to `publish_attempts`. Post status is reconciled from channel results. Disabled channels are skipped; if all assigned channels are disabled, the post is marked failed.

Retry policy for publish failures:

- max retries with backoff: `1m`, `5m`, `15m`
- while retries remain: status stays `scheduled`
- after retries exhausted (or hard non-retryable error): status becomes `failed`

`send-now` (UI and API) ignores `scheduled_at`, attempts immediately, and uses the same retry bookkeeping.

## LinkedIn Publishing Notes

`linkedin` mode is optional and config-gated.

- LinkedIn publishing APIs may require specific product access and approvals.
- Channel tests use a live token probe (`/v2/userinfo`) from `/settings/channels` and `/api/v1/channels/{id}/test`.
- Channel credentials can be rotated through UI/API update endpoints using explicit secret actions: `keep`, `replace`, `clear`.
- For development/testing, `dry-run` mode logs intended publish actions and marks success in scheduler flow.

## Facebook Page Publishing Notes

`facebook-page` mode is optional and config-gated.

- Requires a valid Facebook Page access token (`FACEBOOK_PAGE_ACCESS_TOKEN`) and page ID (`FACEBOOK_PAGE_ID`).
- The publisher posts to `/{page-id}/feed` on the configured Graph API base URL.
- 429/5xx responses are treated as retryable; other API errors are treated as terminal.
- Channel tests use a live page probe (`/{page-id}?fields=id,name`) from `/settings/channels` and `/api/v1/channels/{id}/test`.
- Channel credentials can be rotated through UI/API update endpoints using explicit secret actions: `keep`, `replace`, `clear`.

## Debian/Ubuntu Deployment (systemd)

1. Build binaries:

   ```bash
   make build
   sudo install -m 0755 bin/linkedin-cron-server /usr/local/bin/linkedin-cron-server
   sudo install -m 0755 bin/linkedin-cron-scheduler /usr/local/bin/linkedin-cron-scheduler
   ```

2. Install app files (example path):

   ```bash
   sudo mkdir -p /opt/linkedin-cron
   sudo cp -r web migrations /opt/linkedin-cron/
   ```

3. Create environment file:

   ```bash
   sudo cp .env.example /etc/linkedin-cron.env
   sudoedit /etc/linkedin-cron.env
   ```

4. Install systemd units:

   ```bash
   sudo cp systemd/linkedin-cron.service /etc/systemd/system/
   sudo cp systemd/linkedin-cron-scheduler.service /etc/systemd/system/
   sudo cp systemd/linkedin-cron.timer /etc/systemd/system/
   sudo systemctl daemon-reload
   ```

5. Enable and start:

   ```bash
   sudo systemctl enable --now linkedin-cron.service
   sudo systemctl enable --now linkedin-cron.timer
   ```

6. Verify:

   ```bash
   systemctl status linkedin-cron.service
   systemctl status linkedin-cron.timer
   systemctl status linkedin-cron-scheduler.service
   ```

## Why Two Services + One Timer?

Even though naming often highlights `linkedin-cron.service` and `linkedin-cron.timer`, production setup also includes:

- `linkedin-cron.service` (HTTP server)
- `linkedin-cron-scheduler.service` (oneshot scheduler run)
- `linkedin-cron.timer` (triggers scheduler service every minute)

This keeps operational behavior explicit and avoids hidden in-process worker loops.

## Docker + GHCR Deployment

This repo includes:

- `Dockerfile`
- `docker-compose.yml`
- `docker-compose.public.yml`
- GitHub Actions GHCR publisher: `.github/workflows/ghcr.yml`

Run locally with Docker:

```bash
cp .env.example .env
docker compose up -d
```

Compose behavior:

- `linkedin-cron-server` runs the HTTP app.
- `linkedin-cron-scheduler` overrides image entrypoint and runs the scheduler every minute in a lightweight shell loop.

Or via Makefile:

```bash
make docker-up
```

Pull from GHCR:

```bash
docker pull ghcr.io/joeblack2k/stroopwafel-linkedin-cron:latest
```

### Pull-only host updates (no local Docker build)

Use a GitHub Personal Access Token with at least `read:packages` scope.

```bash
export GHCR_USERNAME=joeblack2k
export GHCR_TOKEN=ghp_xxx
export IMAGE_TAG=latest
./scripts/deploy-ghcr.sh
```

This uses `docker-compose.ghcr.yml` to force image pulls and disable compose local build.

### Public URL compose (username/password + bot API key defaults)

Use the included public compose file:

```bash
docker compose -f docker-compose.public.yml up -d
```

Defaults in that file:

- UI/API Basic Auth: `admin` / `admin`
- Static bot key: `bot:bot-change-me` (set `APP_STATIC_API_KEYS` to your real key)

If you want different credentials/keys in public mode, edit `docker-compose.public.yml` before deploy.

## Import from Postiz

Import LinkedIn login + queued calendar posts from Postiz into this app:

```bash
make import-postiz
```

What it does:

- starts/uses Postiz PostgreSQL container (`postiz-postgres`)
- imports/updates one LinkedIn channel (`Imported Postiz LinkedIn`)
- copies queued LinkedIn posts as `scheduled` posts
- attaches imported posts to the imported LinkedIn channel

Optional overrides:

- `APP_URL` (default `http://localhost:8080`)
- `CHANNEL_DISPLAY_NAME`
- `LINKEDIN_AUTHOR_URN_OVERRIDE`
- `POSTIZ_COMPOSE_FILE`
- `KEEP_POSTIZ_PG_RUNNING=1` (keep source Postiz DB container running after import)

The import script needs `sudo docker` access to inspect Postiz data.
