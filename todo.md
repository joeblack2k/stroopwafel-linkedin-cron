# TODO (Postiz parity: API-first -> GUI)

Current verified score: `100/100`.

## P0 — reach parity gate (`>=80`) first

- [x] **+6** Add idempotency keys for mutating API endpoints
  - Persist key/request hash/response replay records.
  - Enforce on mutating `POST|PUT|DELETE /api/v1/*` routes.
  - Added tests for replay + payload mismatch conflict.
- [x] Update parity score after merge (`docs/phase1-backlog.md`, `README.md`, `docs/agents-usage.md`).

## P1 — API hardening after gate

- [x] **+6** Add pagination/filter/search for `GET /api/v1/posts` and `GET /api/v1/channels`.
- [x] **+6** Publish OpenAPI spec + stable machine-readable error code catalog.
- [x] **+8** Add publish lifecycle webhooks with delivery status tracking.

## P2 — GUI parity follow-through

- [x] **+5** Add GUI analytics dashboard (weekly snapshot + channel delivery breakdown).
- [x] Add webhook health panel in Settings + `GET /api/v1/settings/webhooks`.
- [ ] Add webhook retry dashboard + manual replay tools.
