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
2. **Create scheduled post with channels**
   - `POST /api/v1/posts` with `status=scheduled`, `scheduled_at`, `channel_ids`
3. **Operate delivery**
   - `POST /api/v1/posts/{id}/send-now`
   - `POST /api/v1/posts/{id}/reschedule`
   - `POST /api/v1/posts/{id}/send-and-delete`
4. **Inspect and recover**
   - `GET /api/v1/posts/{id}/attempts`
   - `POST /api/v1/posts/{id}/attempts/{attempt_id}/retry`
   - `POST /api/v1/posts/{id}/attempts/{attempt_id}/screenshot`
5. **Use batch workflows when needed**
   - `POST /api/v1/posts/bulk/channels`
   - `POST /api/v1/posts/bulk/send-now`

## Idempotency behavior (mutating endpoints)

- Send `Idempotency-Key: <unique-key>` with mutating API requests.
- Same key + same payload returns stored response with `X-Idempotent-Replay: true`.
- Same key + different payload returns `409` conflict.
- Use a fresh key per logical action from an agent workflow step.

## API parity checklist (weighted)

> Source of truth for full scoring: `docs/phase1-backlog.md`.

- [x] **8** Auth + API key lifecycle
- [x] **10** Post CRUD + schedule/send-now/reschedule/send-and-delete
- [x] **10** Channel CRUD + test + enable/disable + secret rotation
- [x] **12** Attempts/proof/retry/audit endpoints
- [x] **6** Bulk operations with partial-failure payload
- [x] **8** Guardrails + channel rules
- [x] **6** Idempotency keys for mutating endpoints
- [ ] **6** Pagination/filter/search on list endpoints
- [ ] **8** Publish lifecycle webhooks
- [ ] **6** OpenAPI + stable error code catalog

**Current API score:** `60/70`

## Parity gate status

- Project gate requirement: `>=80/100`.
- Current project score: `90/100`.
- Gate status: `met`.
