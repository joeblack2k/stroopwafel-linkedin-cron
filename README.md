# linkedin-cron

A lightweight Go monolith to draft, schedule, and publish LinkedIn posts with:

- server-rendered HTML UI (`net/http` + `html/template` + HTMX)
- minimal authenticated JSON API for agent use
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
- `PUBLISHER_MODE` (`dry-run` or `linkedin`, default `dry-run`)
- `LINKEDIN_ACCESS_TOKEN`
- `LINKEDIN_AUTHOR_URN`
- `LINKEDIN_API_BASE_URL` (default `https://api.linkedin.com`)

If `PUBLISHER_MODE=linkedin` but required LinkedIn vars are missing, the app falls back to dry-run mode.

## UI Endpoints

- `GET /healthz`
- `GET /calendar?view=month|week&date=YYYY-MM-DD`
- `GET /posts/new`
- `POST /posts`
- `GET /posts/{id}/edit`
- `POST /posts/{id}`
- `POST /posts/{id}/delete`
- `POST /posts/{id}/send-now`
- `GET /settings`

## JSON API Endpoints

- `GET /api/v1/healthz`
- `GET /api/v1/posts`
- `GET /api/v1/posts/{id}`
- `POST /api/v1/posts`
- `PUT /api/v1/posts/{id}`
- `DELETE /api/v1/posts/{id}`
- `POST /api/v1/posts/{id}/send-now`
- `GET /api/v1/settings/status`

Notes:

- all API requests/responses are JSON
- timestamps are RFC3339
- API errors use `{ "error": "..." }`

## Scheduler & Retry Behavior

Every run selects posts where:

- `status='scheduled'` and `scheduled_at <= now` with `next_retry_at IS NULL`
- or `next_retry_at <= now`

Batch size is 100 per run.

Retry policy for publish failures:

- max retries with backoff: `1m`, `5m`, `15m`
- while retries remain: status stays `scheduled`
- after retries exhausted (or hard non-retryable error): status becomes `failed`

`send-now` (UI and API) ignores `scheduled_at`, attempts immediately, and uses the same retry bookkeeping.

## LinkedIn Publishing Notes

`linkedin` mode is optional and config-gated.

- LinkedIn publishing APIs may require specific product access and approvals.
- For development/testing, `dry-run` mode logs intended publish actions and marks success in scheduler flow.

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
