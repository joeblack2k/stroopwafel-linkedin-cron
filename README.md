# stroopwafel-linkedin-cron

Lightweight Go monolith to draft, schedule, and publish social posts (LinkedIn + Facebook Pages) with:

- server-rendered HTML UI (`net/http` + `html/template` + HTMX)
- authenticated JSON API for agents
- SQLite storage with handwritten SQL
- pluggable publisher (default: dry-run)
- built-in minute scheduler (inside the container runtime wrapper)

## Core Architecture

- `cmd/server`: HTTP UI + API
- `cmd/scheduler`: one-shot due post processor
- `internal/db`: SQLite schema/migrations/store logic
- `internal/scheduler`: send/retry orchestration
- `internal/linkedin`: LinkedIn publisher
- `internal/facebook`: Facebook Page publisher

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

API keys are stored hashed in SQLite.

## UI Endpoints

- `GET /login`
- `POST /login`
- `GET /logout`
- `POST /logout`
- `GET /healthz`
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

## Scheduler Behavior

Retry backoff:

- 1m
- 5m
- 15m

For channel-assigned posts, attempts are tracked per `(post, channel)` in `publish_attempts`.

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
