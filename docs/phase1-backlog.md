# Phase 1 Backlog (Postiz-parity track)

## Goal

Bring the product closer to Postiz by introducing channel management and post-to-channel assignment.

## Sprint A (implemented in this iteration)

1. **Channel domain + storage**
   - Add `channels` table
   - Add `post_channels` table
   - Add store methods for CRUD/test/assignment

2. **Web GUI channel management**
   - Add `/settings/channels`
   - Create/test/delete channels

3. **Channel API endpoints**
   - `GET /api/v1/channels`
   - `POST /api/v1/channels`
   - `DELETE /api/v1/channels/{id}`
   - `POST /api/v1/channels/{id}/test`

4. **Composer integration**
   - Add channel checkboxes on post create/edit
   - Persist `channel_ids` relations
   - Expose `channel_ids` in post API responses

## Sprint B (next)

1. **Per-channel scheduler execution**
   - Create `publish_attempts` table
   - Process `(post, channel)` targets independently
   - Preserve retry bookkeeping per channel

2. **Channel connection tests (real API probes)**
   - LinkedIn: token/author probe endpoint
   - Facebook: page token/page probe endpoint

3. **Channel-level observability**
   - Add channel fields to scheduler logs
   - Add channel status cards in UI

4. **Guardrails and UX polish**
   - Require at least one channel for scheduled posts
   - Validation hints based on selected channel type
