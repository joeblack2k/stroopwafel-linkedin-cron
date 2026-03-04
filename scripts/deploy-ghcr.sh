#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_ARGS=(-f "${ROOT_DIR}/docker-compose.yml" -f "${ROOT_DIR}/docker-compose.ghcr.yml")

if ! command -v docker >/dev/null 2>&1; then
  echo "docker is required" >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose plugin is required" >&2
  exit 1
fi

: "${GHCR_USERNAME:=joeblack2k}"
: "${IMAGE_TAG:=latest}"

if [[ -n "${GHCR_TOKEN_FILE:-}" ]]; then
  if [[ ! -f "${GHCR_TOKEN_FILE}" ]]; then
    echo "GHCR_TOKEN_FILE not found: ${GHCR_TOKEN_FILE}" >&2
    exit 1
  fi
  GHCR_TOKEN="$(<"${GHCR_TOKEN_FILE}")"
fi

if [[ -z "${GHCR_TOKEN:-}" ]]; then
  cat >&2 <<'ERR'
GHCR_TOKEN is not set.
Use a GitHub Personal Access Token with at least `read:packages` scope.
ERR
  exit 1
fi

echo "[ghcr] logging in as ${GHCR_USERNAME}"
printf '%s' "${GHCR_TOKEN}" | docker login ghcr.io -u "${GHCR_USERNAME}" --password-stdin >/dev/null

echo "[ghcr] pulling ghcr.io/joeblack2k/stroopwafel-social-media-manager:${IMAGE_TAG}"
IMAGE_TAG="${IMAGE_TAG}" docker compose "${COMPOSE_ARGS[@]}" pull

echo "[ghcr] deploying containers"
IMAGE_TAG="${IMAGE_TAG}" docker compose "${COMPOSE_ARGS[@]}" up -d

IMAGE_TAG="${IMAGE_TAG}" docker compose "${COMPOSE_ARGS[@]}" ps
