#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-shortcut}"
DEPLOYMENT="${2:-shortcut-app-service}"
HPA_NAME="${3:-shortcut-app-service-hpa}"
WATCH_SECONDS="${4:-0}"

err() {
  echo "error: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

need_cmd kubectl
need_cmd awk

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CALC_SCRIPT="$SCRIPT_DIR/calc_safe_max_pods.sh"
[[ -x "$CALC_SCRIPT" ]] || err "missing executable $CALC_SCRIPT"

run_check_once() {
  local calc_output safe_max current_replicas desired_replicas hpa_current hpa_desired

  calc_output="$($CALC_SCRIPT "$NAMESPACE" "$DEPLOYMENT" "$HPA_NAME")"
  safe_max="$(echo "$calc_output" | awk -F= '/^safeMaxPods=/{print $2}')"

  [[ -n "$safe_max" ]] || err "failed to parse safeMaxPods"

  current_replicas="$(kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" -o jsonpath='{.status.replicas}')"
  desired_replicas="$(kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" -o jsonpath='{.status.readyReplicas}')"
  hpa_current="$(kubectl -n "$NAMESPACE" get hpa "$HPA_NAME" -o jsonpath='{.status.currentReplicas}')"
  hpa_desired="$(kubectl -n "$NAMESPACE" get hpa "$HPA_NAME" -o jsonpath='{.status.desiredReplicas}')"

  current_replicas="${current_replicas:-0}"
  desired_replicas="${desired_replicas:-0}"
  hpa_current="${hpa_current:-0}"
  hpa_desired="${hpa_desired:-0}"

  if (( hpa_desired <= safe_max && hpa_current <= safe_max && current_replicas <= safe_max )); then
    echo "PASS safeMaxPods=$safe_max deployReplicas=$current_replicas readyReplicas=$desired_replicas hpaCurrent=$hpa_current hpaDesired=$hpa_desired"
    return 0
  fi

  echo "FAIL safeMaxPods=$safe_max deployReplicas=$current_replicas readyReplicas=$desired_replicas hpaCurrent=$hpa_current hpaDesired=$hpa_desired"
  return 1
}

if [[ "$WATCH_SECONDS" =~ ^[0-9]+$ ]] && (( WATCH_SECONDS > 0 )); then
  echo "watching every ${WATCH_SECONDS}s (Ctrl+C to stop)"
  while true; do
    date '+%Y-%m-%dT%H:%M:%S%z'
    run_check_once || true
    sleep "$WATCH_SECONDS"
  done
else
  run_check_once
fi
