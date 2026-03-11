# Agent API Usage (Parity track)

This runbook is for agents that operate the API while we close Postiz parity gaps.

## Auth model

- Preferred for agents: `X-API-Key` header.
- Supported alternatives: `Authorization: Bearer ...` or HTTP Basic Auth.
- Key lifecycle: create/revoke via `/settings` (UI) or `POST /api/v1/settings/bot-handoff`.

## API-first operating flow

1. **Ensure channel exists**
   - `GET /api/v1/channels`
   - `POST /api/v1/channels`
   - `POST /api/v1/channels/{id}/rotate-credentials`
2. **Create scheduled post with channels**
   - `POST /api/v1/posts` with `status=scheduled`, `scheduled_at`, `channel_ids`
   - If `accept_before_planning=true` and auth is API key, the first scheduling request for a post is stored as draft with `approval_pending=true` until approved in UI (`/approvals`).
   - After that post has been approved once, later API edits and reschedules for the same post do not require another approval.
3. **Operate delivery**
   - `POST /api/v1/posts/{id}/send-now`
   - `POST /api/v1/posts/{id}/reschedule`
   - `POST /api/v1/posts/{id}/send-and-delete`
4. **Inspect and recover**
   - `GET /api/v1/settings/webhooks`
   - `GET /api/v1/webhooks/replays`
   - `GET /api/v1/webhooks/dead-letters`
   - `GET /api/v1/webhooks/dead-letters/alerts`
   - `POST /api/v1/webhooks/replays/{id}/replay`
   - `POST /api/v1/webhooks/replays/{id}/cancel`
   - `POST /api/v1/webhooks/replays/replay-failed`
   - `GET /api/v1/posts/{id}/attempts`
   - `POST /api/v1/posts/{id}/attempts/{attempt_id}/retry`
   - `POST /api/v1/posts/{id}/attempts/{attempt_id}/screenshot`
5. **Use batch workflows when needed**
   - `POST /api/v1/posts/bulk/channels`
   - `POST /api/v1/posts/bulk/send-now`

## Pagination/filter/search

- `GET /api/v1/posts` supports: `limit`, `offset`, `status`, `channel_id`, `q`, `scheduled_from`, `scheduled_to`.
- `GET /api/v1/channels` supports: `limit`, `offset`, `type`, `status`, `q`.
- Both list endpoints return `{ "items": [...], "pagination": {...} }`.

## Idempotency behavior (mutating endpoints)

- Send `Idempotency-Key: <unique-key>` with mutating API requests.
- Same key + same payload returns stored response with `X-Idempotent-Replay: true`.
- Same key + different payload returns `409` conflict.
- Use a fresh key per logical action from an agent workflow step.

## Approval boundaries

- Read-only endpoints (`GET`) never require planning approval.
- Non-scheduling mutating calls can proceed normally with an API key.
- Planning approval is only for the first transition of a post into a scheduled state when `accept_before_planning=true`.

## Webhook lifecycle events

- Configure endpoints via `APP_WEBHOOK_URLS` (comma-separated `http(s)` URLs).
- Optional signing secret via `APP_WEBHOOK_SECRET`.
- Emitted events:
  - `publish.attempt.created`
  - `post.state.changed`
- Headers:
  - `X-Stroopwafel-Event`
  - `X-Stroopwafel-Event-Id`
  - `X-Stroopwafel-Timestamp`
  - `X-Stroopwafel-Signature` (when secret configured)
- Delivery telemetry endpoint for agents: `GET /api/v1/settings/webhooks`.
- Replay queue endpoints: `GET /api/v1/webhooks/replays`, `POST /api/v1/webhooks/replays/{id}/replay`, `POST /api/v1/webhooks/replays/replay-failed`.

## OpenAPI + error catalog

- OpenAPI YAML endpoint: `GET /api/v1/meta/openapi`
- Error catalog endpoint: `GET /api/v1/meta/error-codes`
- Error response shape: `{ "error": "...", "error_code": "..." }`

## API parity checklist (weighted)

> Source of truth for full scoring: `docs/phase1-backlog.md`.

- [x] **8** Auth + API key lifecycle
- [x] **10** Post CRUD + schedule/send-now/reschedule/send-and-delete
- [x] **10** Channel CRUD + test + enable/disable + secret rotation
- [x] **12** Attempts/proof/retry/audit endpoints
- [x] **6** Bulk operations with partial-failure payload
- [x] **8** Guardrails + channel rules
- [x] **6** Idempotency keys for mutating endpoints
- [x] **6** Pagination/filter/search on list endpoints
- [x] **8** Publish lifecycle webhooks
- [x] **6** OpenAPI + stable error code catalog

**Current API score:** `70/70`

## Parity gate status

- Project gate requirement: `>=80/100`.
- Current project score: `100/100`.
- Gate status: `met`.
