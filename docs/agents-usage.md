# Agent API Usage Guide

This guide explains how agents should authenticate and safely use the JSON API.

## Authentication model

- UI routes use HTTP Basic Auth.
- API routes accept:
  - HTTP Basic Auth, or
  - API key (`X-API-Key` or `Authorization: Bearer ...`).

## Create an API key in the GUI

1. Login to `/settings` with Basic Auth.
2. In **Agent API keys**, enter a key name (for example: `nightly-agent`).
3. Click **Create API key**.
4. Copy the shown key immediately (it is only shown once).

## Revoke an API key

In `/settings`, click **Revoke** next to the key.

Revoked keys are rejected immediately for API requests.

## Recommended request pattern for agents

Use `X-API-Key`:

```bash
curl -H "X-API-Key: lcak_xxx" http://localhost:8080/api/v1/posts
```

## Channel-first workflow (important)

Scheduled posts must include at least one `channel_id`.

List channels:

```bash
curl -H "X-API-Key: lcak_xxx" http://localhost:8080/api/v1/channels
```

Create a dry-run channel (safe default for automation tests):

```bash
curl -X POST http://localhost:8080/api/v1/channels \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{
    "type": "dry-run",
    "display_name": "agent-dry-run"
  }'
```

Create a scheduled post with channel assignment:

```bash
curl -X POST http://localhost:8080/api/v1/posts \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{
    "text": "Agent generated update",
    "status": "scheduled",
    "scheduled_at": "2026-03-03T12:00:00Z",
    "channel_ids": [1]
  }'
```

Send now:

```bash
curl -X POST -H "X-API-Key: lcak_xxx" http://localhost:8080/api/v1/posts/1/send-now
```

## Bulk operations

Bulk send now:

```bash
curl -X POST http://localhost:8080/api/v1/posts/bulk/send-now \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{"post_ids": [1,2,3]}'
```

Bulk set channels:

```bash
curl -X POST http://localhost:8080/api/v1/posts/bulk/channels \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{"post_ids": [1,2,3], "channel_ids": [1]}'
```

## Delivery history

Per-post channel attempt history:

```bash
curl -H "X-API-Key: lcak_xxx" \
  "http://localhost:8080/api/v1/posts/1/attempts?status=retry&limit=50"
```

## Security recommendations for agents

- Store API keys in secret managers, never in source control.
- Create one key per agent (least privilege + better audit trail).
- Rotate keys on a fixed schedule.
- Revoke keys immediately when an agent is retired or compromised.
