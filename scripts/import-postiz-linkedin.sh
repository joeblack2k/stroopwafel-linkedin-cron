#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTIZ_COMPOSE_FILE="${POSTIZ_COMPOSE_FILE:-/opt/stacks/management/postiz/compose.yaml}"
POSTIZ_PG_CONTAINER="${POSTIZ_PG_CONTAINER:-postiz-postgres}"
APP_URL="${APP_URL:-http://localhost:8080}"
CHANNEL_DISPLAY_NAME="${CHANNEL_DISPLAY_NAME:-Imported Postiz LinkedIn}"
LINKEDIN_API_BASE_URL="${LINKEDIN_API_BASE_URL:-https://api.linkedin.com}"

set -a
if [[ -f "${ROOT_DIR}/.env" ]]; then
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
fi
set +a

: "${APP_BASIC_AUTH_USER:?APP_BASIC_AUTH_USER missing (.env)}"
: "${APP_BASIC_AUTH_PASS:?APP_BASIC_AUTH_PASS missing (.env)}"

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required" >&2
  exit 1
fi

postiz_was_running="0"
if sudo docker ps --format '{{.Names}}' | grep -qx "${POSTIZ_PG_CONTAINER}"; then
  postiz_was_running="1"
fi

cleanup() {
  if [[ "${postiz_was_running}" == "0" && "${KEEP_POSTIZ_PG_RUNNING:-0}" != "1" ]]; then
    sudo docker compose -f "${POSTIZ_COMPOSE_FILE}" stop postiz-postgres >/dev/null || true
    echo "[import] stopped temporary Postiz postgres container"
  fi
}
trap cleanup EXIT

echo "[import] ensuring Postiz postgres is running"
sudo docker compose -f "${POSTIZ_COMPOSE_FILE}" up -d postiz-postgres >/dev/null

integration_row="$({
  sudo docker exec "${POSTIZ_PG_CONTAINER}" \
    psql -U postiz-user -d postiz-db-local -At -F $'\t' -P pager=off -c \
    "SELECT COALESCE(name,''), COALESCE(\"internalId\",''), COALESCE(token,''), disabled
     FROM \"Integration\"
     WHERE \"providerIdentifier\"='linkedin' AND \"deletedAt\" IS NULL
     ORDER BY \"createdAt\" DESC
     LIMIT 1;"
} || true)"

if [[ -z "${integration_row}" ]]; then
  echo "[import] no LinkedIn integration found in Postiz" >&2
  exit 1
fi

IFS=$'\t' read -r integration_name internal_id access_token disabled_flag <<<"${integration_row}"

if [[ -z "${access_token}" ]]; then
  echo "[import] LinkedIn integration token is empty; nothing to import" >&2
  exit 1
fi

if [[ -n "${LINKEDIN_AUTHOR_URN_OVERRIDE:-}" ]]; then
  author_urn="${LINKEDIN_AUTHOR_URN_OVERRIDE}"
elif [[ -n "${internal_id}" ]]; then
  author_urn="urn:li:person:${internal_id}"
else
  echo "[import] unable to derive linkedin_author_urn; set LINKEDIN_AUTHOR_URN_OVERRIDE" >&2
  exit 1
fi

if [[ "${disabled_flag}" == "t" ]]; then
  echo "[import] warning: source integration is disabled in Postiz, importing anyway"
fi

auth=("-u" "${APP_BASIC_AUTH_USER}:${APP_BASIC_AUTH_PASS}")

echo "[import] ensuring LinkedIn channel exists in Stroopwafel: Social Media Manager"
channels_json="$(curl -fsS "${auth[@]}" "${APP_URL}/api/v1/channels")"
channel_id="$(jq -r --arg name "${CHANNEL_DISPLAY_NAME}" '.[] | select(.type=="linkedin" and .display_name==$name) | .id' <<<"${channels_json}" | head -n1)"

if [[ -z "${channel_id}" ]]; then
  payload="$(jq -n \
    --arg type "linkedin" \
    --arg display_name "${CHANNEL_DISPLAY_NAME}" \
    --arg token "${access_token}" \
    --arg urn "${author_urn}" \
    --arg api "${LINKEDIN_API_BASE_URL}" \
    '{
      type: $type,
      display_name: $display_name,
      linkedin_access_token: $token,
      linkedin_author_urn: $urn,
      linkedin_api_base_url: $api
    }')"
  create_resp="$(curl -fsS "${auth[@]}" -H 'Content-Type: application/json' -X POST "${APP_URL}/api/v1/channels" -d "${payload}")"
  channel_id="$(jq -r '.id' <<<"${create_resp}")"
else
  update_payload="$(jq -n \
    --arg display_name "${CHANNEL_DISPLAY_NAME}" \
    --arg urn "${author_urn}" \
    --arg api "${LINKEDIN_API_BASE_URL}" \
    --arg token "${access_token}" \
    '{
      display_name: $display_name,
      linkedin_author_urn: $urn,
      linkedin_api_base_url: $api,
      linkedin_access_token_action: "replace",
      linkedin_access_token: $token
    }')"
  curl -fsS "${auth[@]}" -H 'Content-Type: application/json' -X PUT "${APP_URL}/api/v1/channels/${channel_id}" -d "${update_payload}" >/dev/null
fi

if [[ -z "${channel_id}" || "${channel_id}" == "null" ]]; then
  echo "[import] failed to resolve destination channel id" >&2
  exit 1
fi

echo "[import] channel id: ${channel_id}"

existing_posts_json="$(curl -fsS "${auth[@]}" "${APP_URL}/api/v1/posts")"

post_rows="$({
  sudo docker exec "${POSTIZ_PG_CONTAINER}" \
    psql -U postiz-user -d postiz-db-local -At -P pager=off -c \
    "SELECT row_to_json(x)
     FROM (
       SELECT
         p.id AS source_post_id,
         to_char(p.\"publishDate\", 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"') AS scheduled_at,
         p.content,
         COALESCE(p.image, '[]') AS image_json
       FROM \"Post\" p
       JOIN \"Integration\" i ON i.id=p.\"integrationId\"
       WHERE i.\"providerIdentifier\"='linkedin'
         AND p.\"deletedAt\" IS NULL
         AND p.state='QUEUE'
       ORDER BY p.\"publishDate\" ASC
     ) x;"
} || true)"

if [[ -z "${post_rows}" ]]; then
  echo "[import] no queued LinkedIn posts found in Postiz"
  exit 0
fi

created=0
skipped=0

while IFS= read -r row; do
  [[ -z "${row}" ]] && continue

  source_post_id="$(jq -r '.source_post_id' <<<"${row}")"
  scheduled_at="$(jq -r '.scheduled_at' <<<"${row}")"
  text="$(jq -r '.content' <<<"${row}")"
  image_json="$(jq -rc '.image_json' <<<"${row}")"

  [[ -z "${source_post_id}" || "${source_post_id}" == "null" ]] && continue

  if jq -e --arg text "${text}" --arg scheduled_at "${scheduled_at}" 'any(.[]; .text == $text and ((.scheduled_at // "") == $scheduled_at))' <<<"${existing_posts_json}" >/dev/null; then
    skipped=$((skipped + 1))
    continue
  fi

  media_url="$(jq -r 'try (if type=="array" and length > 0 then (.[0].url // .[0].src // .[0]) else empty end) catch empty' <<<"${image_json}")"
  if [[ "${media_url}" != http://* && "${media_url}" != https://* ]]; then
    media_url=""
  fi

  payload="$(jq -n \
    --arg text "${text}" \
    --arg status "scheduled" \
    --arg scheduled_at "${scheduled_at}" \
    --arg media_url "${media_url}" \
    --argjson channel_ids "[${channel_id}]" \
    'if $media_url == "" then
        {
          text: $text,
          status: $status,
          scheduled_at: $scheduled_at,
          channel_ids: $channel_ids
        }
      else
        {
          text: $text,
          status: $status,
          scheduled_at: $scheduled_at,
          media_url: $media_url,
          channel_ids: $channel_ids
        }
      end')"

  created_resp="$(curl -fsS "${auth[@]}" -H 'Content-Type: application/json' -X POST "${APP_URL}/api/v1/posts" -d "${payload}")"
  existing_posts_json="$(jq --argjson item "${created_resp}" '. + [$item]' <<<"${existing_posts_json}")"
  created=$((created + 1))
done <<<"${post_rows}"

echo "[import] done: created=${created}, skipped_duplicates=${skipped}, channel_id=${channel_id}"
