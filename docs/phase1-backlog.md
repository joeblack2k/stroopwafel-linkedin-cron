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

## Sprint D (implemented)

1. **Credential UX hardening (phase 1 implemented)**
   - Added explicit secret masking metadata to channel API responses (`secret_preview`, `secret_presence`)
   - Added structured audit metadata viewer on `/settings/channels/{id}/edit` (parsed JSON + raw fallback)

2. **History UX polish (phase 2 implemented)**
   - Added date-range filtering (`attempted_from`, `attempted_to`) for `/api/v1/posts/{id}/attempts`
   - Added date-range controls on `/posts/{id}/history`

3. **Bulk UX polish (phase 3 implemented)**
   - Added selection memory in `/posts/bulk` using lightweight browser `localStorage`
   - Added explicit bulk confirmation guardrail (client + server)
   - Added partial-failure retry helper with failed-post preselection on redirect

4. **Bulk UX polish (phase 4 implemented)**
   - Added server-side bulk filters (`status`, `q`) on `/posts/bulk`
   - Preserved filter context across bulk action redirects and retries


## Sprint E (implemented)

1. **Calendar UX upgrade**
   - Added `view=list` calendar mode with ready-date summary + full queue cards
   - Added compact card labels (`LINKEDIN POST`, `FACEBOOK POST`, `MULTI CHANNEL POST`)
   - Added card actions: `view post`, `edit post`, `send and delete`

2. **Drag & drop rescheduling**
   - Added week time-grid UI (vertical hour rows)
   - Added drag-drop interaction in month and week views
   - Added `POST /posts/{id}/reschedule` endpoint

3. **Post detail flow**
   - Added `GET /posts/{id}` post detail page
   - Added `POST /posts/{id}/send-and-delete` convenience action

4. **Settings bot handoff UX**
   - Added one-click bot handoff action in `/settings`
   - Added `POST /settings/api-keys/bot-handoff` route
   - Generates API key + copyable instruction payload for agents

5. **Channel setup wizard polish**
   - Reworked `/settings/channels` into a guided 3-step channel setup wizard
   - Added platform-specific hints and dynamic form visibility for LinkedIn/Facebook/Dry-run


## Sprint F (implemented)

1. **Friendly login flow for public URL deployments**
   - Added `/login` username/password form
   - Added signed HttpOnly session cookie auth for UI routes
   - Added `/logout` endpoint and navigation links
   - Preserved API auth model (Basic + API keys)

2. **Session security hardening**
   - Session cookies use `HttpOnly`, `SameSite=Lax`
   - `Secure` cookie flag controlled by `APP_SESSION_SECURE`
   - Session tokens are HMAC-signed and time-bounded

## Sprint G (implemented)

1. **Agent API ergonomics**
   - Added `POST /api/v1/posts/{id}/send-and-delete`
   - Added `POST /api/v1/posts/{id}/reschedule`
   - Added `POST /api/v1/settings/bot-handoff`

2. **Public UX continuity improvements**
   - Made static assets public so `/login` is fully styled without auth
   - Added logout affordance in UI navigation
   - Expanded docs + tests for new agent/UI flows


## Sprint H (implemented)

1. **Data-first container model**
   - Moved runtime persistence model to `/data`
   - Added `/data/config.json` bootstrap/load support in app config
   - Kept SQLite and all user-generated state under `/data`

2. **Single-container Docker UX**
   - Added container entrypoint wrapper running server + minute scheduler loop
   - Simplified compose to one service with only port + `/data` bind mount
   - Added `docker-compose.example.yml` with host path placeholder

3. **Deployment consistency**
   - Updated docs and status views to expose `data_dir` + `config_path`
   - Maintained API/UI behavior while removing docker env sprawl

## Sprint I (Agent MVP hardening)

- Added proof-of-post metadata to attempts: `permalink`, `error_category`, `screenshot_url`.
- Added API endpoints for attempt screenshot attachment and one-click retry.
- Added scheduling guardrail checks (duplicate slot + tight spacing warnings).
- Added per-channel posting rules (`max_text_length`, `max_hashtags`, `required_phrase`).
- Added weekly snapshot API endpoint for planning + delivery counters.
