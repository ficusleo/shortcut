package webapi

import (
	"context"
	"encoding/json"
	"net/http"
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

	// ⚠️ Критическая точка: если канал полон — горутина ЗАБЛОКИРУЕТСЯ!
	select {
	case h.daemon.TaskQueue <- task:
		// Успешно добавлено в очередь
	default:
		http.Error(w, "Task queue is full, try again later", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusAccepted)
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
		h.metrics.Recorder.IncHTTPResponseStatus(status)
		http.Error(w, "Failed to format response", status)
		return
	}
	h.metrics.Recorder.IncHTTPResponseStatus(status)
	w.Write(formattedResponse)
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
