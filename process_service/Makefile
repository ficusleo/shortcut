SHELL := /usr/bin/env bash

NAMESPACE ?= shortcut
SERVICE ?= shortcut-app-service
DEPLOYMENT ?= shortcut-app-service
HPA ?= shortcut-app-service-hpa

WATCH ?= 5

CPU_ITER ?= 20
CPU_WORKERS ?= 4
CPU_SECONDS ?= 90

MEM_ITER ?= 12
MEM_MB ?= 192
MEM_SECONDS ?= 120

.PHONY: hpa-help hpa-safe hpa-check hpa-watch hpa-cpu hpa-mem

hpa-help:
	@echo "HPA experiment targets:"
	@echo "  make hpa-safe                  # calculate safeMaxPods"
	@echo "  make hpa-check                 # one-shot PASS/FAIL"
	@echo "  make hpa-watch WATCH=5         # watch PASS/FAIL"
	@echo "  make hpa-cpu                   # CPU load + watcher"
	@echo "  make hpa-mem                   # memory load + watcher"
	@echo ""
	@echo "Tunable variables (defaults):"
	@echo "  NAMESPACE=$(NAMESPACE)"
	@echo "  SERVICE=$(SERVICE)"
	@echo "  DEPLOYMENT=$(DEPLOYMENT)"
	@echo "  HPA=$(HPA)"
	@echo "  WATCH=$(WATCH)"
	@echo "  CPU_ITER=$(CPU_ITER)"
	@echo "  CPU_WORKERS=$(CPU_WORKERS)"
	@echo "  CPU_SECONDS=$(CPU_SECONDS)"
	@echo "  MEM_ITER=$(MEM_ITER)"
	@echo "  MEM_MB=$(MEM_MB)"
	@echo "  MEM_SECONDS=$(MEM_SECONDS)"
	@echo ""
	@echo "Example: make hpa-cpu WATCH=3 CPU_ITER=30 CPU_WORKERS=6 CPU_SECONDS=120"

hpa-safe:
	chmod +x scripts/calc_safe_max_pods.sh
	./scripts/calc_safe_max_pods.sh $(NAMESPACE) $(DEPLOYMENT) $(HPA)

hpa-check:
	chmod +x scripts/check_hpa_bounds.sh
	./scripts/check_hpa_bounds.sh $(NAMESPACE) $(DEPLOYMENT) $(HPA)

hpa-watch:
	chmod +x scripts/check_hpa_bounds.sh
	./scripts/check_hpa_bounds.sh $(NAMESPACE) $(DEPLOYMENT) $(HPA) $(WATCH)

hpa-cpu:
	chmod +x scripts/run_hpa_experiment_check.sh
	./scripts/run_hpa_experiment_check.sh $(NAMESPACE) $(SERVICE) $(DEPLOYMENT) $(HPA) cpu $(WATCH) $(CPU_ITER) $(CPU_WORKERS) $(CPU_SECONDS)

hpa-mem:
	chmod +x scripts/run_hpa_experiment_check.sh
	./scripts/run_hpa_experiment_check.sh $(NAMESPACE) $(SERVICE) $(DEPLOYMENT) $(HPA) memory $(WATCH) $(MEM_ITER) $(CPU_WORKERS) $(CPU_SECONDS) $(MEM_MB) $(MEM_SECONDS)
