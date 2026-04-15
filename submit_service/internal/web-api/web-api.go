package webapi

import (
	"context"
	"net/http"
	"time"

	log "github.com/sirupsen/logrus"

	"submit_service/internal/metrics"
	"submit_service/internal/services"
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

type API struct {
	logger *log.Logger
	server *http.Server
}

func New(ctx context.Context, conf *Config, taskSrv *services.TaskService, taskBus TaskBus, m *metrics.Service, logger *log.Logger) *API {
	cpuLoadHandler := NewCPULoadHandler()
	readinessHandler := NewReadinessHandler()
	memoryLoadHandler := NewMemoryLoadHandler()
	metricsHandler := NewMetricsHandler(taskSrv, m)
	tasksHandler := NewTaskHandler(taskSrv, taskBus, m)

	mux := http.NewServeMux()
	mux.HandleFunc(_submitPath, tasksHandler.SubmitTask)
	mux.HandleFunc(_readinessPath, readinessHandler.HandleReadiness)
	mux.HandleFunc(_metricsPath, metricsHandler.LogMetrics)
	mux.HandleFunc(_cpuLoadPath, cpuLoadHandler.CPULoadHandler)
	mux.HandleFunc(_memoryLoadPath, memoryLoadHandler.MemoryLoadHandler)

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
