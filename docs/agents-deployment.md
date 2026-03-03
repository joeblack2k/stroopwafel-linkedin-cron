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

## 2) Required environment values

At minimum set in `.env`:

- `APP_BASIC_AUTH_USER`
- `APP_BASIC_AUTH_PASS`
- `APP_DB_PATH=/data/linkedin-cron.db`

Optional publisher settings:

- LinkedIn mode (`PUBLISHER_MODE=linkedin`, token + author URN)
- Facebook Page mode (`PUBLISHER_MODE=facebook-page`, page token + page id)

## 3) Health checks

```bash
curl http://localhost:8080/healthz
curl -u "$APP_BASIC_AUTH_USER:$APP_BASIC_AUTH_PASS" http://localhost:8080/api/v1/healthz
```

## 4) Updating

```bash
docker compose pull
docker compose up -d
```

## 5) Production note

Pin to explicit versions (`vX.Y.Z`) in production instead of `latest`.
