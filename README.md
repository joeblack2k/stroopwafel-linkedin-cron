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

Security behavior:

- API keys are generated once and shown once.
- Only a hash is stored in SQLite (`api_keys.key_hash`).
- Revoked keys are immediately blocked.

## UI Endpoints

- `GET /healthz`
- `GET /calendar?view=month|week&date=YYYY-MM-DD`
- `GET /posts/new`
- `POST /posts`
- `GET /posts/{id}/edit`
- `POST /posts/{id}`
- `POST /posts/{id}/delete`
- `POST /posts/{id}/send-now`
- `GET /posts/{id}/history`
- `GET /posts/bulk`
- `POST /posts/bulk/channels`
- `POST /posts/bulk/send-now`
- `GET /settings`
- `GET /settings/channels`
- `POST /settings/channels`
- `GET /settings/channels/{id}/edit`
- `POST /settings/channels/{id}`
- `POST /settings/channels/{id}/test`
- `POST /settings/channels/{id}/disable`
- `POST /settings/channels/{id}/enable`
- `POST /settings/channels/{id}/delete`

## JSON API Endpoints

- `GET /api/v1/healthz`
- `GET /api/v1/posts`
- `GET /api/v1/posts/{id}`
- `POST /api/v1/posts`
- `PUT /api/v1/posts/{id}`
- `DELETE /api/v1/posts/{id}`
- `POST /api/v1/posts/{id}/send-now`
- `GET /api/v1/posts/{id}/attempts`
- `POST /api/v1/posts/bulk/send-now`
- `POST /api/v1/posts/bulk/channels`
- `GET /api/v1/settings/status`
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
- `GET /api/v1/posts/{id}/attempts` is paginated (`limit`, `offset`) and returns `{items, pagination}`
- `GET /api/v1/channels/{id}/audit` is paginated (`limit`, `offset`) and returns `{items, pagination}`

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
- GitHub Actions GHCR publisher: `.github/workflows/ghcr.yml`

Run locally with Docker:

```bash
cp .env.example .env
docker compose up -d
```

Or via Makefile:

```bash
make docker-up
```

Pull from GHCR:

```bash
docker pull ghcr.io/joeblack2k/stroopwafel-linkedin-cron:latest
```
