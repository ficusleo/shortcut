# HPA experiment plan (minikube)

This runbook validates autoscaling for `shortcut-app-service` based on CPU and memory pressure.

## 1) Prepare minikube

```bash
minikube start --cpus=4 --memory=8192
minikube addons enable metrics-server
kubectl create namespace shortcut || true
```

## 2) Build and deploy app

Use Docker daemon from minikube so the deployment can pull `shortcut_app:latest` with `IfNotPresent`.

```bash
eval $(minikube docker-env)
docker build -t shortcut_app:latest .
kubectl apply -f clickhouse.yaml
kubectl apply -f app.yaml
kubectl -n shortcut rollout status deploy/shortcut-app-service
```

## 3) Confirm requests/limits and HPA

```bash
kubectl -n shortcut get deploy shortcut-app-service -o yaml | grep -A12 resources:
kubectl -n shortcut get hpa
kubectl -n shortcut get hpa shortcut-app-service-hpa -w
```

Current per-pod resources (from `app.yaml`):
- request: `150m` CPU, `256Mi` memory
- limit: `400m` CPU, `512Mi` memory
- HPA: min `1`, max `6`, targets: CPU `75%`, memory `80%`

## 4) Compute safe max pods vs cluster capacity

Quick way (recommended):

```bash
chmod +x scripts/calc_safe_max_pods.sh
./scripts/calc_safe_max_pods.sh
```

This prints `safeMaxPods` and all intermediate values.

Get allocatable node resources:

```bash
kubectl get node -o jsonpath='{.items[0].status.allocatable.cpu}{"\n"}{.items[0].status.allocatable.memory}{"\n"}'
```

Compute upper bounds:

- CPU bound by requests:
  - `maxPodsByCPU = floor(nodeAllocatableCPUm / 150m)`
- Memory bound by requests:
  - `maxPodsByMem = floor(nodeAllocatableMemMi / 256Mi)`
- Effective bound for this deployment:
  - `safeMaxPods = min(maxPodsByCPU, maxPodsByMem, 6)`

`safeMaxPods` is the number you should compare against while testing.

## 5) Trigger CPU pressure

Expose app locally:

```bash
kubectl -n shortcut port-forward svc/shortcut-app-service 8080:8080
```

In another shell, start CPU load routines:

```bash
for i in {1..20}; do
  curl -s "http://127.0.0.1:8080/load/cpu?workers=4&seconds=90" >/dev/null &
done
wait
```

Observe scaling:

```bash
kubectl -n shortcut get pods -l app=shortcut-app-service -w
kubectl -n shortcut top pods -l app=shortcut-app-service
kubectl -n shortcut get hpa shortcut-app-service-hpa
```

## 6) Trigger memory pressure

```bash
for i in {1..12}; do
  curl -s "http://127.0.0.1:8080/load/memory?mb=192&seconds=120" >/dev/null &
done
wait
```

Then check:

```bash
kubectl -n shortcut top pods -l app=shortcut-app-service
kubectl -n shortcut get hpa shortcut-app-service-hpa
kubectl -n shortcut get deploy shortcut-app-service
```

## 7) Validate experiment result

All-in-one mode (start load + watch PASS/FAIL together):

```bash
chmod +x scripts/run_hpa_experiment_check.sh
# CPU experiment
./scripts/run_hpa_experiment_check.sh shortcut shortcut-app-service shortcut-app-service shortcut-app-service-hpa cpu 5 20 4 90

# Memory experiment
./scripts/run_hpa_experiment_check.sh shortcut shortcut-app-service shortcut-app-service shortcut-app-service-hpa memory 5 12 4 90 192 120
```

Quick PASS/FAIL checks:

```bash
chmod +x scripts/check_hpa_bounds.sh
./scripts/check_hpa_bounds.sh
```

Watch during active load (every 5s):

```bash
./scripts/check_hpa_bounds.sh shortcut shortcut-app-service shortcut-app-service-hpa 5
```

Pass criteria:

1. Replicas increase above `1` when utilization exceeds HPA targets.
2. Replicas never exceed `safeMaxPods` from step 4.
3. Replicas never exceed HPA `maxReplicas` (`6`).

Useful troubleshooting:

```bash
kubectl -n shortcut describe hpa shortcut-app-service-hpa
kubectl -n shortcut describe deploy shortcut-app-service
kubectl -n kube-system logs deploy/metrics-server
```

## 8) Load endpoints added in app

- `GET /load/cpu?workers=<n>&seconds=<n>`
- `GET /load/memory?mb=<n>&seconds=<n>`

Both return `202 Accepted` and start the load asynchronously.
