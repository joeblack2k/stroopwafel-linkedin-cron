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

## Sprint C (next)

1. **Credential UX hardening**
   - Optional per-channel credential rotation flow
   - Soft-disable channel without deleting history

2. **History views**
   - UI/API view for per-channel publish attempts per post
   - Filter by status/date/channel

3. **Bulk operations**
   - Bulk assign channels to selected posts
   - Bulk schedule send-now
