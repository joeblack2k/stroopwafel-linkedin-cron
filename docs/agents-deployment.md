# Agent Deployment Guide (Docker + GHCR)

This project publishes a container image to GHCR through `.github/workflows/ghcr.yml`.

## 1) Pull and run from GHCR

```bash
docker pull ghcr.io/joeblack2k/stroopwafel-linkedin-cron:latest
```

For a full local stack (server + minute scheduler), use `docker-compose.yml`:

```bash
cp .env.example .env
docker compose up -d
```

The compose setup runs:

- `linkedin-cron-server` (web UI + API)
- `linkedin-cron-scheduler` (separate process that runs every 60s)

Both containers share the same SQLite volume (`linkedin_cron_data`).

For public URL deployments with explicit auth defaults use `docker-compose.public.yml`:

```bash
docker compose -f docker-compose.public.yml up -d
```

Default auth values in that compose:

- Basic auth: `admin/admin`
- Static API key for bots: `bot:bot-change-me` via `APP_STATIC_API_KEYS`

For production, edit `docker-compose.public.yml` and replace both defaults before exposing the service publicly.

### Pull-only deploy/update (recommended for hosts)

Use `scripts/deploy-ghcr.sh` with a PAT that has `read:packages`:

```bash
export GHCR_USERNAME=joeblack2k
export GHCR_TOKEN=ghp_xxx
export IMAGE_TAG=latest
./scripts/deploy-ghcr.sh
```

Notes:

- script uses `docker-compose.yml` + `docker-compose.ghcr.yml`
- forces pull (`pull_policy: always`) and disables local compose build
- keeps scheduler as a separate container process
- `GHCR_TOKEN` must include `read:packages` (without it, GHCR pull returns 403)

## 2) Required environment values

At minimum set in `.env`:

- `APP_BASIC_AUTH_USER`
- `APP_BASIC_AUTH_PASS`
- `APP_DB_PATH=/data/linkedin-cron.db`
- `APP_SESSION_SECURE` (set `true` behind HTTPS/reverse proxy)

Optional publisher settings:

- LinkedIn mode (`PUBLISHER_MODE=linkedin`, token + author URN)
- Facebook Page mode (`PUBLISHER_MODE=facebook-page`, page token + page id)

Optional bot API key bootstrap:

- `APP_STATIC_API_KEYS=bot-main:lcak_prod_xxx`



UI login:

- Login UI is available on `/login` (username/password)
- Session cookies are HttpOnly + SameSite=Lax
- `/logout` clears the session cookie

## 3) Health checks

```bash
curl http://localhost:8080/healthz
curl -u "$APP_BASIC_AUTH_USER:$APP_BASIC_AUTH_PASS" http://localhost:8080/api/v1/healthz
```

## 4) Updating

```bash
./scripts/deploy-ghcr.sh
```

## 5) Production note

Pin to explicit versions (`vX.Y.Z`) in production instead of `latest`.

## 6) Import from existing Postiz

Import LinkedIn integration and queued calendar posts from Postiz:

```bash
make import-postiz
```

The import script reads Postiz data from `/opt/stacks/management/postiz`, creates/updates an imported LinkedIn channel, and migrates queued posts into scheduled posts in this app.

If Postiz DB was initially stopped, the script will stop it again after import (set `KEEP_POSTIZ_PG_RUNNING=1` to keep it running).
