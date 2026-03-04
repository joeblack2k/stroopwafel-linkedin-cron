# Agent Deployment Guide (Data-first Docker)

This deployment model keeps **all user data in `/data`**.

## Host data path

Recommended host path:

- `/opt/stacks/tools/stroopwafel`

Mount it as `/data` in the container.

## Minimal compose

```yaml
services:
  stroopwafel:
    image: ghcr.io/joeblack2k/stroopwafel-social-media-manager:latest
    container_name: stroopwafel
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - /opt/stacks/tools/stroopwafel:/data
```

## What is persisted in `/data`

- `/data/config.json`
- `/data/stroopwafel.db`
- `/data/stroopwafel.db-wal`
- `/data/stroopwafel.db-shm`

This includes channel credentials (in DB), API keys (hashed in DB), scheduler state, and app settings.

## First boot behavior

If `/data/config.json` does not exist, the app auto-creates it with defaults:

- user/pass: `admin/admin`
- timezone: `UTC`
- publisher mode: `dry-run`

## Runtime process model in container

The image entrypoint starts:

- HTTP server
- scheduler loop (runs scheduler command every 60 seconds)

So one container is enough.

## Pull-only GHCR update

```bash
export GHCR_USERNAME=joeblack2k
export GHCR_TOKEN=ghp_xxx
export IMAGE_TAG=latest
./scripts/deploy-ghcr.sh
```

Requirements:

- `GHCR_TOKEN` with `read:packages`

## Login/UI auth

- UI login page: `/login`
- Session cookie: HttpOnly + SameSite=Lax
- `Secure` flag controlled by `APP_SESSION_SECURE`

## Health checks

```bash
curl http://localhost:8080/healthz
curl -u admin:admin http://localhost:8080/api/v1/healthz
```
