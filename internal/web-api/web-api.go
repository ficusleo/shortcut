package webapi

import (
	"context"
	"encoding/json"
	"net/http"

	log "github.com/sirupsen/logrus"

	"shortcut/internal/daemon"
	"shortcut/internal/metrics"
)

type Handler struct {
	daemon  *daemon.Daemon
	metrics *metrics.Metrics
}

type API struct {
	logger *log.Logger
	server *http.Server
}

func (h *Handler) WithDaemon(d *daemon.Daemon) *Handler {
	h.daemon = d
	return h
}

func (h *Handler) WithMetrics(m *metrics.Metrics) *Handler {
	h.metrics = m
	return h
}

func (h *Handler) SubmitTask(w http.ResponseWriter, r *http.Request) {
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
	resp := h.metrics.GetMetrics()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	formattedResponse, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		http.Error(w, "Failed to format response", http.StatusInternalServerError)
		return
	}
	w.Write(formattedResponse)
}

func New(d *daemon.Daemon, m *metrics.Metrics, logger *log.Logger) *API {
	h := &Handler{}
	h.WithDaemon(d).WithMetrics(m)

	mux := http.NewServeMux()
	mux.HandleFunc("/submit", h.SubmitTask)
	mux.HandleFunc("/metrics", h.MetricsHandler)

	server := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	return &API{
		logger: logger,
		server: server,
	}
}

func (api *API) Start() {
	api.logger.Info("Server started on :8080")
	api.logger.Info("Try: hey -n 15000 -c 100 http://localhost:8080/submit")
	go func() {
		err := api.server.ListenAndServe()
		if err != nil {
			api.logger.WithError(err).Error("HTTP server error")
		}
	}()
}

func (api *API) Stop() {
	err := api.server.Shutdown(context.Background())
	if err != nil {
		api.logger.WithError(err).Error("Failed to shut down server")
	}
	api.logger.Info("Server shut down")
}
