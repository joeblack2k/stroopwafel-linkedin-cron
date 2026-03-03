# Phase 1 Backlog (Postiz parity: API-first -> GUI)

## Defined scope (for scoring)

In scope:

- Agent-first API for scheduling, channels, retries, and operations.
- Operator GUI for calendar, channel setup, bulk actions, and settings.
- Reliability signals needed for day-to-day publishing.

Out of scope (for this phase):

- AI writing/assistant features
- social inbox / comment management
- team approval workflows / RBAC
- billing or paid analytics exports

## Weighted checklist (100 points)

### API-first checklist (70 points)

- [x] **8** Auth + API key lifecycle (`Basic`, `X-API-Key`, create/revoke, bot handoff)
- [x] **10** Post CRUD + schedule/send-now/reschedule/send-and-delete
- [x] **10** Channel CRUD + test + enable/disable + secret rotation (`keep|replace|clear`)
- [x] **12** Delivery attempts + proof fields + retry endpoint + channel audit endpoint
- [x] **6** Bulk API operations with partial-failure envelope (`requested/succeeded/failed/errors`)
- [x] **8** Scheduling guardrails + per-channel posting rules
- [x] **6** Idempotency keys on mutating API endpoints (`Idempotency-Key` replay + mismatch conflict)
- [ ] **6** Pagination/filter/search for `GET /api/v1/posts` and `GET /api/v1/channels`
- [ ] **8** Publish lifecycle webhooks (delivery events)
- [ ] **6** OpenAPI contract + stable machine-readable error catalog

**API subtotal:** `60/70`

### GUI checklist (30 points)

- [x] **8** Calendar month/week/list, drag-drop reschedule, send-and-delete actions
- [x] **7** Channel wizard + edit flow + rules + audit history page
- [x] **5** Bulk UI with selection memory, confirmation guardrail, retry prefill
- [x] **5** Login/session flow + settings + API key + bot handoff UX
- [x] **5** Analytics dashboard in GUI (weekly snapshot + delivery breakdown)

**GUI subtotal:** `30/30`

## Score summary

- **Current verified score:** `90/100`
- **Gate target:** `>=80/100`
- **Gate status:** `met`

## Execution plan (API-first -> GUI)

### Phase A (P0) — parity gate

1. ✅ Add idempotency key support for mutating `/api/v1/*` routes.
2. ✅ Add replay/conflict tests for identical key behavior.
3. ✅ Recompute score and update docs (`todo.md`, `README.md`, `docs/agents-usage.md`).

### Phase B (P1) — API parity hardening

1. Add pagination/filter/search for post and channel list APIs.
2. Publish OpenAPI spec and error code catalog.
3. Add outbound webhook events for publish lifecycle.

### Phase C (P2) — GUI parity follow-through

1. Add webhook delivery health/status panel in Settings.
2. Add richer analytics slices (date/channel filters) without increasing frontend weight.
