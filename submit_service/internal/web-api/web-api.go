package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"shortcut/internal/daemon"
	"shortcut/internal/metrics"
)

const (
	_readinessPath    = "/readiness"
	_submitPath       = "/submit"
	_metricsPath      = "/metrics"
	_cpuLoadPath      = "/load/cpu"
	_memoryLoadPath   = "/load/memory"
	_readinessTimeout = 5 * time.Second
)

type Config struct {
	Addr string `mapstructure:"addr"`
}

type Handler struct {
	daemon  *daemon.Daemon
	metrics *metrics.Service
}

type API struct {
	logger *log.Logger
	server *http.Server
}

func (h *Handler) WithDaemon(d *daemon.Daemon) *Handler {
	h.daemon = d
	return h
}

func (h *Handler) WithMetrics(m *metrics.Service) *Handler {
	h.metrics = m
	return h
}

func (h *Handler) SubmitTask(w http.ResponseWriter, r *http.Request) {
	if isShuttingDown.Load() {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}

	task := &daemon.Task{ID: h.daemon.NewTaskID()}
	status := http.StatusAccepted
	// ⚠️ Критическая точка: если канал полон — горутина ЗАБЛОКИРУЕТСЯ!
	select {
	case h.daemon.TaskQueue <- task:
		// Успешно добавлено в очередь
	default:
		status = http.StatusServiceUnavailable
		http.Error(w, "Task queue is full, try again later", status)
		h.metrics.Recorder.IncHTTPResponseStatus(status)
		return
	}
	h.metrics.Recorder.IncHTTPResponseStatus(status)

	w.WriteHeader(status)
}

func (h *Handler) MetricsHandler(w http.ResponseWriter, r *http.Request) {
	resp := h.metrics.Recorder.GetMetrics()
	resp["not_processed_tasks_count"] = uint64(len(h.daemon.Ch.GetAllNotProcessedTasks()))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	formattedResponse, err := json.MarshalIndent(resp, "", "  ")
	status := http.StatusAccepted
	if err != nil {
		status = http.StatusInternalServerError
		http.Error(w, "Failed to format response", status)
		return
	}
	w.Write(formattedResponse)
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("value must be a positive integer")
	}
	return v, nil
}

func runCPULoad(workers int, duration time.Duration) {
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := range workers {
		go func(offset int) {
			defer wg.Done()
			value := float64(offset + 1)
			for time.Now().Before(deadline) {
				value = math.Sqrt(value*1.000001 + 123.456)
				if value > 100000 {
					value = 1
				}
			}
		}(i)
	}

	wg.Wait()
}

func runMemoryLoad(megabytes int, duration time.Duration) {
	chunk := make([]byte, megabytes*1024*1024)
	for i := 0; i < len(chunk); i += 4096 {
		chunk[i] = byte(i)
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(duration)
	for {
		select {
		case <-timeout:
			runtime.KeepAlive(chunk)
			return
		case <-ticker.C:
			for i := 0; i < len(chunk); i += 4096 {
				chunk[i]++
			}
		}
	}
}

func (h *Handler) CPULoadHandler(w http.ResponseWriter, r *http.Request) {
	workers, err := parsePositiveInt(r.URL.Query().Get("workers"), runtime.NumCPU())
	if err != nil {
		http.Error(w, "workers must be a positive integer", http.StatusBadRequest)
		return
	}
	seconds, err := parsePositiveInt(r.URL.Query().Get("seconds"), 30)
	if err != nil {
		http.Error(w, "seconds must be a positive integer", http.StatusBadRequest)
		return
	}

	go runCPULoad(workers, time.Duration(seconds)*time.Second)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "cpu load started",
		"workers": workers,
		"seconds": seconds,
	})
}

func (h *Handler) MemoryLoadHandler(w http.ResponseWriter, r *http.Request) {
	megabytes, err := parsePositiveInt(r.URL.Query().Get("mb"), 128)
	if err != nil {
		http.Error(w, "mb must be a positive integer", http.StatusBadRequest)
		return
	}
	seconds, err := parsePositiveInt(r.URL.Query().Get("seconds"), 30)
	if err != nil {
		http.Error(w, "seconds must be a positive integer", http.StatusBadRequest)
		return
	}

	go runMemoryLoad(megabytes, time.Duration(seconds)*time.Second)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "memory load started",
		"mb":      megabytes,
		"seconds": seconds,
	})
}

var isShuttingDown atomic.Bool

func readinessHandler(w http.ResponseWriter, r *http.Request) {
	if isShuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("shutting down"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func New(conf *Config, d *daemon.Daemon, m *metrics.Service, logger *log.Logger) *API {
	h := &Handler{}
	h.WithDaemon(d).WithMetrics(m)

	mux := http.NewServeMux()
	mux.HandleFunc(_submitPath, h.SubmitTask)
	mux.HandleFunc(_readinessPath, readinessHandler)
	mux.HandleFunc(_metricsPath, h.MetricsHandler)
	mux.HandleFunc(_cpuLoadPath, h.CPULoadHandler)
	mux.HandleFunc(_memoryLoadPath, h.MemoryLoadHandler)

	server := &http.Server{
		Addr:    conf.Addr,
		Handler: mux,
	}

	return &API{
		logger: logger,
		server: server,
	}
}

func (api *API) Start() {
	api.logger.Infof("Server started on %s", api.server.Addr)
	api.logger.Info("Try: hey -n 15000 -c 100 http://localhost:8080/submit")
	go func() {
		err := api.server.ListenAndServe()
		if err != nil {
			api.logger.WithError(err).Error("HTTP server error")
		}
	}()
}

func (api *API) Stop(ctx context.Context) error {
	isShuttingDown.Store(true)
	api.logger.Info("Readiness probe set to unhealthy, waiting for traffic to drain...")

	// Give some time for LB/Kubernetes to detect the probe failure and stop sending new traffic.
	// 5 seconds is a typical value, but it depends on the infrastructure.
	select {
	case <-time.After(_readinessTimeout):
	case <-ctx.Done():
	}

	err := api.server.Shutdown(ctx)
	if err != nil {
		api.logger.WithError(err).Error("Failed to shut down server")
		return err
	}
	api.logger.Info("Server shut down")
	return nil
}
