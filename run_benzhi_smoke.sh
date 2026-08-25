#!/usr/bin/env bash
# PotatoEye smoke test: builds the server, starts it locally, probes health and
# exercises the real lock/get-task API, then cleans up every process and temp
# file. Deterministic, no external network access.
set -euo pipefail

tmpdir="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmpdir"
}
trap cleanup EXIT

bin="$tmpdir/potatoeye"
db="$tmpdir/potatoeye.db"
port="${POTATOEYE_SMOKE_PORT:-18080}"
base="http://127.0.0.1:$port"

echo "building potatoeye..."
go build -o "$bin" ./cmd

POTATOEYE_ADDR="127.0.0.1:$port" POTATOEYE_DB="$db" "$bin" &
server_pid=$!

up=0
for _ in $(seq 1 100); do
  if curl -s -o /dev/null "$base/healthz" 2>/dev/null; then
    up=1
    break
  fi
  sleep 0.1
done
if [[ "$up" != "1" ]]; then
  echo "server did not become healthy" >&2
  exit 1
fi

health="$(curl -s "$base/healthz")"
if [[ "$health" != *'"status":"ok"'* ]]; then
  echo "health check failed: $health" >&2
  exit 1
fi

lock_body='{"operation_id":"smoke-1","plot":"P-01","variety":"V-G2","cellar":"C-01","disinfect":"D-01","batch":"B-SMOKE","basket_seals":["seal-1","seal-2"],"blind_codes":["code-1","code-2"],"healing_shed":"shed-1","probe_window":"probe-1","sprout_slot":"slot-1","test_wells":["well-1"],"points":["p-1","p-2"],"reviewers":["reviewer-1","reviewer-2"]}'
lock_resp="$(curl -s -X POST "$base/v1/tasks/lock" -H 'Content-Type: application/json' -d "$lock_body")"
if [[ "$lock_resp" != *'"task_id"'* ]]; then
  echo "lock failed: $lock_resp" >&2
  exit 1
fi

task_id="$(printf '%s' "$lock_resp" | sed -n 's/.*"task_id":"\([^"]*\)".*/\1/p')"
if [[ -z "$task_id" ]]; then
  echo "could not parse task id from: $lock_resp" >&2
  exit 1
fi

get_resp="$(curl -s "$base/v1/tasks/$task_id")"
if [[ "$get_resp" != *"$task_id"* ]]; then
  echo "get task failed: $get_resp" >&2
  exit 1
fi

echo "smoke ok: locked task $task_id"
