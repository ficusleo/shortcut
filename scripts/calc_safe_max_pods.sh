#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${1:-shortcut}"
DEPLOYMENT="${2:-shortcut-app-service}"
HPA_NAME="${3:-shortcut-app-service-hpa}"

err() {
  echo "error: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || err "required command not found: $1"
}

cpu_to_millicores() {
  local value="$1"
  if [[ "$value" == *m ]]; then
    echo "${value%m}"
    return
  fi
  awk -v v="$value" 'BEGIN { printf "%d", v * 1000 }'
}

mem_to_mib() {
  local value="$1"
  case "$value" in
    *Ki)
      awk -v v="${value%Ki}" 'BEGIN { printf "%d", v / 1024 }'
      ;;
    *Mi)
      echo "${value%Mi}"
      ;;
    *Gi)
      awk -v v="${value%Gi}" 'BEGIN { printf "%d", v * 1024 }'
      ;;
    *Ti)
      awk -v v="${value%Ti}" 'BEGIN { printf "%d", v * 1024 * 1024 }'
      ;;
    *)
      err "unsupported memory unit: $value (expected Ki, Mi, Gi, or Ti)"
      ;;
  esac
}

need_cmd kubectl
need_cmd awk

cpu_request_raw="$(kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" -o jsonpath='{.spec.template.spec.containers[0].resources.requests.cpu}')"
mem_request_raw="$(kubectl -n "$NAMESPACE" get deploy "$DEPLOYMENT" -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')"
max_replicas_raw="$(kubectl -n "$NAMESPACE" get hpa "$HPA_NAME" -o jsonpath='{.spec.maxReplicas}')"

[[ -n "$cpu_request_raw" ]] || err "CPU request is empty for deployment $DEPLOYMENT"
[[ -n "$mem_request_raw" ]] || err "memory request is empty for deployment $DEPLOYMENT"
[[ -n "$max_replicas_raw" ]] || err "maxReplicas is empty for HPA $HPA_NAME"

cpu_request_m="$(cpu_to_millicores "$cpu_request_raw")"
mem_request_mib="$(mem_to_mib "$mem_request_raw")"
max_replicas="$max_replicas_raw"

mapfile -t allocatable < <(kubectl get nodes -o jsonpath='{range .items[*]}{.status.allocatable.cpu}{"\t"}{.status.allocatable.memory}{"\n"}{end}')

[[ "${#allocatable[@]}" -gt 0 ]] || err "no nodes found in cluster"

total_cpu_m=0
total_mem_mib=0

for line in "${allocatable[@]}"; do
  node_cpu="${line%%$'\t'*}"
  node_mem="${line#*$'\t'}"

  node_cpu_m="$(cpu_to_millicores "$node_cpu")"
  node_mem_mib="$(mem_to_mib "$node_mem")"

  total_cpu_m=$((total_cpu_m + node_cpu_m))
  total_mem_mib=$((total_mem_mib + node_mem_mib))
done

max_pods_by_cpu=$((total_cpu_m / cpu_request_m))
max_pods_by_mem=$((total_mem_mib / mem_request_mib))

safe_max_pods="$max_pods_by_cpu"
if (( max_pods_by_mem < safe_max_pods )); then
  safe_max_pods="$max_pods_by_mem"
fi
if (( max_replicas < safe_max_pods )); then
  safe_max_pods="$max_replicas"
fi

echo "namespace=$NAMESPACE"
echo "deployment=$DEPLOYMENT"
echo "hpa=$HPA_NAME"
echo "cpuRequest=$cpu_request_raw ($cpu_request_m m)"
echo "memoryRequest=$mem_request_raw ($mem_request_mib Mi)"
echo "allocatableCPU=$total_cpu_m m"
echo "allocatableMemory=$total_mem_mib Mi"
echo "maxPodsByCPU=$max_pods_by_cpu"
echo "maxPodsByMem=$max_pods_by_mem"
echo "hpaMaxReplicas=$max_replicas"
echo "safeMaxPods=$safe_max_pods"
