# Phase 1 Backlog (Postiz-parity track)

## Goal

Bring the product closer to Postiz by introducing channel management and post-to-channel assignment.

## Sprint A (implemented)

1. **Channel domain + storage**
   - Added `channels` table
   - Added `post_channels` table
   - Added store methods for CRUD/test/assignment

2. **Web GUI channel management**
   - Added `/settings/channels`
   - Added create/test/delete channel flows

3. **Channel API endpoints**
   - `GET /api/v1/channels`
   - `POST /api/v1/channels`
   - `DELETE /api/v1/channels/{id}`
   - `POST /api/v1/channels/{id}/test`

4. **Composer integration**
   - Added channel checkboxes on post create/edit
   - Persisted `channel_ids` relations
   - Exposed `channel_ids` in post API responses

## Sprint B (implemented)

1. **Per-channel scheduler execution**
   - Added `publish_attempts` table
   - Scheduler processes `(post, channel)` independently
   - Retry bookkeeping is tracked per channel
   - Aggregate post status is reconciled from channel outcomes

2. **Channel connection tests (real API probes)**
   - LinkedIn `/v2/userinfo` probe for token validation
   - Facebook Page graph probe (`/{page_id}?fields=id,name`)
   - `/settings/channels/{id}/test` and `/api/v1/channels/{id}/test` run live probe + persist result

3. **Channel-level observability**
   - Scheduler logs include `channel_id`, `channel_type`, and `channel_name`
   - Channels GUI includes status cards (total/active/error/disabled + type counts)

4. **Guardrails and UX polish**
   - Scheduled posts now require at least one channel (UI + API validation)
   - Channel create form includes dynamic type-based validation hints

## Sprint C (implemented)

1. **Credential UX hardening (phase 1)**
   - Soft-disable/enable channel from UI and API
   - Channel edit workflow in UI (`/settings/channels/{id}/edit`)
   - API channel update endpoint (`PUT /api/v1/channels/{id}`)
   - Secret rotation semantics for channel tokens: `keep`, `replace`, `clear`

2. **History views**
   - UI: `GET /posts/{id}/history` with status/channel filters
   - API: `GET /api/v1/posts/{id}/attempts`

3. **Bulk operations**
   - UI: `GET /posts/bulk`
   - UI actions: `POST /posts/bulk/channels`, `POST /posts/bulk/send-now`
   - API actions: `POST /api/v1/posts/bulk/channels`, `POST /api/v1/posts/bulk/send-now`

4. **Audit + pagination (phase 3)**
   - Added `channel_audit_events` persistence
   - Channel updates now write audit records (including actor + metadata)
   - API: `GET /api/v1/channels/{id}/audit` (paginated)
   - API: `GET /api/v1/posts/{id}/attempts` now paginated
   - UI: channel edit page shows audit trail with pagination
   - UI: post history view supports pagination

## Sprint D (in progress)

1. **Credential UX hardening (phase 1 implemented)**
   - Added explicit secret masking metadata to channel API responses (`secret_preview`, `secret_presence`)
   - Added structured audit metadata viewer on `/settings/channels/{id}/edit` (parsed JSON + raw fallback)

2. **History UX polish (next)**
   - Date-range filtering for large histories

3. **Bulk UX polish (next)**
   - Saved selections, safer confirmation UX, and partial-failure retry helpers
