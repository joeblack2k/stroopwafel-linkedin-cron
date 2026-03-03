# Agent API Usage Guide

This guide explains how agents should authenticate and safely use the JSON API.

## Authentication model

- UI routes use HTTP Basic Auth.
- API routes accept:
  - HTTP Basic Auth, or
  - API key (`X-API-Key` or `Authorization: Bearer ...`).

For fully automated deployments, you can preconfigure bot keys with `APP_STATIC_API_KEYS` (for example `bot-main:lcak_prod_xxx`).

## Create an API key in the GUI

1. Login to `/settings` with Basic Auth.
2. In **Agent API keys**, enter a key name (for example: `nightly-agent`).
3. Click **Create API key**.
4. Copy the shown key immediately (it is only shown once).

## One-click bot handoff (recommended)

In `/settings` use **Give this to your bot**.

This flow creates a fresh API key and generates a copyable handoff text that includes:

- base URL
- API key
- auth header examples
- minimal endpoint workflow

Use this when onboarding non-technical users or external agents quickly.

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

Channel responses include:

- `secret_preview` with masked credential values
- `secret_presence` with booleans indicating which credential fields are set

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

Send and delete:

```bash
curl -X POST -H "X-API-Key: lcak_xxx" http://localhost:8080/api/v1/posts/1/send-and-delete
```

Reschedule:

```bash
curl -X POST http://localhost:8080/api/v1/posts/1/reschedule \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{"scheduled_at":"2026-03-03T13:00:00Z"}'
```

## Channel credential rotation

Update channel credentials with explicit action semantics for secrets:

- `keep` = leave existing secret unchanged
- `replace` = set new secret value
- `clear` = remove stored secret

Example (`replace` LinkedIn token, keep others):

```bash
curl -X PUT http://localhost:8080/api/v1/channels/1 \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{
    "display_name": "agent-linkedin-main",
    "linkedin_access_token_action": "replace",
    "linkedin_access_token": "new-token-value",
    "linkedin_author_urn": "urn:li:organization:123"
  }'
```

### API bootstrap handoff endpoint

Agents or operators can also generate a fresh API key via API:

```bash
curl -X POST http://localhost:8080/api/v1/settings/bot-handoff \
  -H "Content-Type: application/json" \
  -u admin:admin \
  -d '{"name":"bot-main"}'
```

This returns:

- `api_key`
- `instructions` (copy-ready handoff text)

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

Bulk API responses return a result envelope:

- `requested`
- `succeeded`
- `failed`
- `errors` (per-post details)

Agents should treat `failed > 0` as partial success and retry only the failed post IDs.

## Delivery history

Per-post channel attempt history (paginated + date-range):

```bash
curl -H "X-API-Key: lcak_xxx" \
  "http://localhost:8080/api/v1/posts/1/attempts?status=retry&attempted_from=2026-03-03T00:00:00Z&attempted_to=2026-03-04T00:00:00Z&limit=50&offset=0"
```

Supported attempt filters:

- `status`
- `channel_id`
- `attempted_from` (RFC3339)
- `attempted_to` (RFC3339)

Response shape:

- `items`: list of attempts
- `pagination`: `{limit, offset, total, has_next, has_prev}`

Attempt items include proof fields:

- `error_category`
- `permalink`
- `screenshot_url`

Attach/replace screenshot URL for an attempt:

```bash
curl -X POST http://localhost:8080/api/v1/posts/1/attempts/10/screenshot \
  -H "Content-Type: application/json" \
  -H "X-API-Key: lcak_xxx" \
  -d '{"screenshot_url":"https://example.com/proof/10.png"}'
```

One-click retry for a failed attempt after fixing credentials/scopes:

```bash
curl -X POST -H "X-API-Key: lcak_xxx" http://localhost:8080/api/v1/posts/1/attempts/10/retry
```

Weekly snapshot (basic planning + delivery metrics):

```bash
curl -H "X-API-Key: lcak_xxx" \
  "http://localhost:8080/api/v1/analytics/weekly-snapshot?start=2026-03-01T00:00:00Z"
```

## Channel audit trail

Every successful channel update writes an audit event.

List channel audit events:

```bash
curl -H "X-API-Key: lcak_xxx" \
  "http://localhost:8080/api/v1/channels/1/audit?limit=25&offset=0"
```

Audit response also returns `{items, pagination}`.

Each event includes:

- `event_type` (for now: `channel.updated`)
- `actor` (basic auth user or API key identifier)
- `summary`
- `metadata` JSON string with changed fields and secret actions

## Security recommendations for agents

- Store API keys in secret managers, never in source control.
- Create one key per agent (least privilege + better audit trail).
- Rotate keys on a fixed schedule.
- Revoke keys immediately when an agent is retired or compromised.
