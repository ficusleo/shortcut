#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-shortcut}"
SERVICE="${2:-shortcut-app-service}"
DEPLOYMENT="${3:-shortcut-app-service}"
HPA_NAME="${4:-shortcut-app-service-hpa}"
MODE="${5:-cpu}"            # cpu | memory
WATCH_SECONDS="${6:-5}"
LOAD_ITERATIONS="${7:-20}"
CPU_WORKERS="${8:-4}"
CPU_SECONDS="${9:-90}"
MEM_MB="${10:-192}"
MEM_SECONDS="${11:-120}"

err() {
  echo "error: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

need_cmd curl
need_cmd kubectl
need_cmd bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CHECK_SCRIPT="$SCRIPT_DIR/check_hpa_bounds.sh"
[[ -x "$CHECK_SCRIPT" ]] || err "missing executable $CHECK_SCRIPT"

[[ "$MODE" == "cpu" || "$MODE" == "memory" ]] || err "MODE must be cpu or memory"
[[ "$WATCH_SECONDS" =~ ^[0-9]+$ ]] || err "WATCH_SECONDS must be integer"
[[ "$LOAD_ITERATIONS" =~ ^[0-9]+$ ]] || err "LOAD_ITERATIONS must be integer"

kubectl -n "$NAMESPACE" get svc "$SERVICE" >/dev/null
kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" >/dev/null
kubectl -n "$NAMESPACE" get hpa "$HPA_NAME" >/dev/null

cleanup() {
  if [[ -n "${PF_PID:-}" ]]; then
    kill "$PF_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "${WATCH_PID:-}" ]]; then
    kill "$WATCH_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

echo "starting port-forward to service/$SERVICE on 127.0.0.1:8080"
kubectl -n "$NAMESPACE" port-forward "svc/$SERVICE" 8080:8080 >/tmp/hpa-port-forward.log 2>&1 &
PF_PID=$!
sleep 2
if ! kill -0 "$PF_PID" >/dev/null 2>&1; then
  err "port-forward failed, check /tmp/hpa-port-forward.log"
fi

echo "starting bounds watch every ${WATCH_SECONDS}s"
"$CHECK_SCRIPT" "$NAMESPACE" "$DEPLOYMENT" "$HPA_NAME" "$WATCH_SECONDS" &
WATCH_PID=$!

if [[ "$MODE" == "cpu" ]]; then
  echo "sending CPU load: iterations=$LOAD_ITERATIONS workers=$CPU_WORKERS seconds=$CPU_SECONDS"
  for _ in $(seq 1 "$LOAD_ITERATIONS"); do
    curl -fsS "http://127.0.0.1:8080/load/cpu?workers=$CPU_WORKERS&seconds=$CPU_SECONDS" >/dev/null &
  done
else
  echo "sending memory load: iterations=$LOAD_ITERATIONS mb=$MEM_MB seconds=$MEM_SECONDS"
  for _ in $(seq 1 "$LOAD_ITERATIONS"); do
    curl -fsS "http://127.0.0.1:8080/load/memory?mb=$MEM_MB&seconds=$MEM_SECONDS" >/dev/null &
  done
fi

wait

echo "load requests completed; watcher is still running"
echo "press Ctrl+C to stop"
wait "$WATCH_PID"
