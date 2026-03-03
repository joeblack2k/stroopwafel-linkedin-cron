#!/bin/sh
set -eu

: "${SCHEDULER_INTERVAL_SECONDS:=60}"

mkdir -p /data

scheduler_loop() {
  while true; do
    /usr/local/bin/linkedin-cron-scheduler || true
    sleep "${SCHEDULER_INTERVAL_SECONDS}"
  done
}

scheduler_loop &
SCHEDULER_PID=$!

/usr/local/bin/linkedin-cron-server &
SERVER_PID=$!

cleanup() {
  kill "${SCHEDULER_PID}" >/dev/null 2>&1 || true
}

trap cleanup INT TERM EXIT

wait "${SERVER_PID}"
STATUS=$?
cleanup
exit "${STATUS}"
