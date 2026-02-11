package main

import (
	"context"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"
	"go.uber.org/dig"

	"shortcut/extapi"
	"shortcut/internal/clickhouse"
	"shortcut/internal/daemon"
	"shortcut/internal/metrics"
	webapi "shortcut/internal/web-api"
)

const (
	numWorkers = 5
	queueSize  = 100
)

func main() {
	container := dig.New()

	// container.Provide(ProvideBaseContext)
	container.Provide(ProvideClickhouse)
	container.Provide(ProvideLogger)
	container.Provide(ProvideMetrics)
	container.Provide(ProvideDaemon)
	container.Provide(ProvideWebAPI)

	if err := container.Invoke(func(m *metrics.Service, d *daemon.Daemon, api *webapi.API, ch *clickhouse.Service) {
		defer stop(d, api, m)
		ctx := context.Background()

		m.Start()
		ch.Start(ctx)
		d.Start(ctx, extapi.New())
		api.Start()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		for {
			select {
			case <-sigCh:
				log.Info("Received shutdown signal, exiting...")
				return
			case err := <-ch.ErrCh:
				log.Errorf("clickhouse error: %v", err)
				return
			}
		}
	}); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

}

func stop(d *daemon.Daemon, api *webapi.API, m *metrics.Service) {
	api.Stop()
	d.Stop()
	m.Stop()
}

func ProvideBaseContext() context.Context {
	return context.Background()
}

func ProvideClickhouse(m *metrics.Service) (*clickhouse.Service, error) {
	// TODO: move to viper config initialization
	chConf := &clickhouse.Config{
		DSN:        "http://localhost:8123",
		NumRetries: 3,
	}
	return clickhouse.NewService(chConf, m)
}

func ProvideLogger(ch *clickhouse.Service) *log.Logger {
	logger := log.New()
	logger.SetLevel(log.DebugLevel)
	hook := clickhouse.NewLogHook(ch.Client)
	logger.AddHook(hook)
	return logger
}

func ProvideMetrics() *metrics.Service {
	svc := metrics.New(&metrics.Config{
		// run metrics on a separate port to avoid collision with web API
		Addr:     ":9090",
		Endpoint: "/metrics",
	})
	if err := svc.Start(); err != nil {
		log.Fatalf("failed to start metrics service: %v", err)
	}
	return svc
}

func ProvideDaemon(ctx context.Context, m *metrics.Service, ch *clickhouse.Service, logger *log.Logger) *daemon.Daemon {
	return daemon.New(ctx, numWorkers, queueSize, m, ch, logger)
}

func ProvideWebAPI(d *daemon.Daemon, m *metrics.Service, logger *log.Logger) *webapi.API {
	return webapi.New(d, m, logger)
}
