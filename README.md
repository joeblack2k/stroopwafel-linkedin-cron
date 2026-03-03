# stroopwafel-linkedin-cron

Lightweight Go monolith to draft, schedule, and publish social posts (LinkedIn + Facebook Pages + Instagram Business) with:

- server-rendered HTML UI (`net/http` + `html/template` + HTMX)
- authenticated JSON API for agents
- SQLite storage with handwritten SQL
- pluggable publisher (default: dry-run)
- built-in minute scheduler (inside the container runtime wrapper)

## Postiz Parity Snapshot (API-first)

- Scope is tracked in `docs/phase1-backlog.md` (API-first, then GUI).
- Current verified score: **100/100** for defined parity scope.
- The `>=80` gate is met for this scope.
- Active implementation checklist: `todo.md`.
- Agent-facing parity runbook: `docs/agents-usage.md`.

## Core Architecture

- `cmd/server`: HTTP UI + API
- `cmd/scheduler`: one-shot due post processor
- `internal/db`: SQLite schema/migrations/store logic
- `internal/scheduler`: send/retry orchestration
- `internal/linkedin`: LinkedIn publisher
- `internal/facebook`: Facebook Page publisher
- `internal/instagram`: Instagram publisher

## Data-first Docker Model (important)

This project is designed so all user state is under **`/data`** in the container.

Persistent files:

- `/data/config.json` → runtime configuration (auth/settings/publisher defaults)
- `/data/linkedin-cron.db` (+ `-wal`, `-shm`) → posts/channels/api keys/history

This means you can delete/update/recreate containers safely as long as your `/data` mount remains.

## Recommended Host Path

Use a host bind mount such as:

- `/opt/stacks/tools/stroopwafel:/data`

## Docker Compose (minimal)

`docker-compose.yml` intentionally only contains:

- one service
- port mapping
- `/data` mount

```yaml
services:
  stroopwafel:
    image: ghcr.io/joeblack2k/stroopwafel-linkedin-cron:latest
    container_name: stroopwafel
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /opt/stacks/tools/stroopwafel:/data
```

Also included:

- `docker-compose.example.yml` with a placeholder absolute path
- `docker-compose.ghcr.yml` for pull-only deployments with tagged images

## First Start

1. Create your data directory on host:

```bash
mkdir -p /opt/stacks/tools/stroopwafel
```

2. Start:

```bash
docker compose up -d
```

3. On first boot, if `/data/config.json` is missing, it is auto-created with defaults.

Default bootstrap values:

- user: `admin`
- pass: `admin`
- timezone: `UTC`
- publisher mode: `dry-run`

## `config.json`

Main settings live in `/data/config.json`. Example:

```json
{
  "version": 1,
  "basic_auth_user": "admin",
  "basic_auth_pass": "admin",
  "timezone": "UTC",
  "publisher_mode": "dry-run",
  "static_api_keys": {
    "lcak_example_token": "bot-main"
  },
  "linkedin_access_token": "",
  "linkedin_author_urn": "",
  "linkedin_api_base_url": "https://api.linkedin.com",
  "facebook_page_access_token": "",
  "facebook_page_id": "",
  "facebook_api_base_url": "https://graph.facebook.com/v22.0"
}
```

If you edit `config.json`, restart the container.

## Webhook Configuration (Env)

Webhook targets are configured with environment variables (no secrets in API responses):

- `APP_WEBHOOK_URLS` (comma-separated `http(s)` URLs)
- `APP_WEBHOOK_SECRET` (optional HMAC signing secret)

Example:

```bash
APP_WEBHOOK_URLS=https://automation.example/hooks/social,https://agent.example/events
APP_WEBHOOK_SECRET=replace-with-random-secret
```

## Local Dev (without Docker)

```bash
cp .env.example .env
make run
```

For local runs, `.env` values bootstrap config if `APP_CONFIG_PATH` does not exist yet.

## Make Targets

- `make build`
- `make run`
- `make run-scheduler`
- `make test`
- `make fmt`
- `make lint`
- `make docker-build`
- `make docker-up`
- `make docker-down`
- `make docker-up-ghcr`
- `make import-postiz`

## Auth Model

### UI

- `/login` username/password form
- session cookie (`HttpOnly`, `SameSite=Lax`, `Secure` via `APP_SESSION_SECURE`)
- `/logout` to clear session

### API

- Basic Auth, or
- API key (`X-API-Key` or `Authorization: Bearer ...`)

Mutating API endpoints (`POST|PUT|DELETE /api/v1/*`) also support `Idempotency-Key`.

API keys are stored hashed in SQLite.

## Agent-Focused MVP Additions

- Proof-of-post log now stores and exposes: status, attempted time, channel, external id, permalink, error category, optional screenshot URL.
- Smart planning guardrails warn on duplicate slots and too-tight scheduling windows (`30m`).
- Channel rules are configurable per channel: `max_text_length`, `max_hashtags`, `required_phrase`.
- Failsafe error categories are persisted for attempts (`auth_expired`, `scope_missing`, `rate_limited`, etc.) with one-click retry endpoints.
- Weekly snapshot endpoint provides planning and delivery counters plus top-post selection.
- Channel capabilities are exposed via API (`supports_media`, `media_types`, `requires_media`) and enforced on create/update/reschedule.
- Media uploads are supported via UI/API upload endpoints and persisted under `/data/uploads` (served at `/media/*`).
- Instagram channels are first-class with dedicated credentials and publisher implementation.
- List endpoints now support pagination and filters (`limit`, `offset`, `q`, status/type filters).
- Publish lifecycle webhooks are emitted for agents (`publish.attempt.created`, `post.state.changed`).
- OpenAPI and error catalog are exposed at API metadata endpoints.

## UI Endpoints

- `GET /login`
- `POST /login`
- `GET /logout`
- `POST /logout`
- `GET /healthz`
- `GET /calendar?view=month|week|list&date=YYYY-MM-DD`
- `GET /analytics`
- `GET /analytics/data`
- `GET /posts/new`
- `POST /posts`
- `POST /media/upload`
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
- `POST /settings/channels/{id}/rules`
- `POST /settings/channels/{id}/test`
- `POST /settings/channels/{id}/disable`
- `POST /settings/channels/{id}/enable`
- `POST /settings/channels/{id}/delete`

## JSON API Endpoints

- `GET /api/v1/healthz`
- `GET /api/v1/posts`
- `GET /api/v1/posts/{id}`
- `POST /api/v1/posts`
- `POST /api/v1/media/upload`
- `POST /api/v1/posts/guardrails`
- `PUT /api/v1/posts/{id}`
- `DELETE /api/v1/posts/{id}`
- `POST /api/v1/posts/{id}/send-now`
- `POST /api/v1/posts/{id}/send-and-delete`
- `POST /api/v1/posts/{id}/reschedule`
- `GET /api/v1/posts/{id}/attempts`
- `POST /api/v1/posts/{id}/attempts/{attempt_id}/screenshot`
- `POST /api/v1/posts/{id}/attempts/{attempt_id}/retry`
- `POST /api/v1/posts/bulk/send-now`
- `POST /api/v1/posts/bulk/channels`
- `GET /api/v1/settings/status`
- `GET /api/v1/meta/openapi`
- `GET /api/v1/meta/error-codes`
- `GET /api/v1/analytics/overview`
- `GET /api/v1/analytics/weekly-snapshot`
- `POST /api/v1/settings/bot-handoff`
- `GET /api/v1/channels`
- `POST /api/v1/channels`
- `PUT /api/v1/channels/{id}`
- `GET /api/v1/channels/{id}/rules`
- `PUT /api/v1/channels/{id}/rules`
- `GET /api/v1/channels/{id}/audit`
- `DELETE /api/v1/channels/{id}`
- `POST /api/v1/channels/{id}/test`
- `POST /api/v1/channels/{id}/disable`
- `POST /api/v1/channels/{id}/enable`

## Scheduler Behavior

Retry backoff:

- 1m
- 5m
- 15m

For channel-assigned posts, attempts are tracked per `(post, channel)` in `publish_attempts`.

Attempt failures include categorized error metadata for retry automation.

## GHCR Deploy

Pull-only update flow:

```bash
export GHCR_USERNAME=joeblack2k
export GHCR_TOKEN=ghp_xxx
export IMAGE_TAG=latest
./scripts/deploy-ghcr.sh
```

Requires PAT scope: `read:packages`.

## Data Migration from Postiz

```bash
make import-postiz
```

Imports LinkedIn integration and queued LinkedIn posts into this app.

## Agent Docs

- `docs/agents-deployment.md`
- `docs/agents-usage.md`
- `docs/phase1-backlog.md`
- `docs/build-brief-fleurtje.md`
- `docs/openapi.yaml`
- `docs/error-catalog.json`
