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
	"shortcut/internal/config"
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

	container.Provide(ProvideConfig)
	container.Provide(ProvideErrorsChan)
	container.Provide(ProvideBaseContext)
	container.Provide(ProvideLogger)
	container.Provide(ProvideClickhouse)
	container.Provide(ProvideMetrics)
	container.Provide(ProvideDaemon)
	container.Provide(ProvideWebAPI)

	container.Provide(func(m *metrics.Service) Stoppable { return m }, dig.Group("stoppables"))
	container.Provide(func(d *daemon.Daemon) Stoppable { return d }, dig.Group("stoppables"))
	container.Provide(func(api *webapi.API) Stoppable { return api }, dig.Group("stoppables"))

	if err := container.Invoke(func(ctx context.Context, args RunArgs) {
		defer stop(ctx, args.Stop)

		args.CH.Start()
		args.D.Start(ctx, extapi.New())
		args.API.Start()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		for {
			select {
			case <-sigCh:
				log.Info("Received shutdown signal, exiting...")
				return
			case err := <-args.CH.ErrCh:
				log.Errorf("clickhouse error: %v", err)
				return
			}
		}
	}); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

}

type RunArgs struct {
	dig.In
	CH   *clickhouse.Service
	D    *daemon.Daemon
	M    *metrics.Service
	API  *webapi.API
	Stop StopArgs
}

type Stoppable interface {
	Stop(context.Context) error
}

type StopArgs struct {
	dig.In
	Stoppables []Stoppable `group:"stoppables"`
}

func stop(ctx context.Context, args StopArgs) {
	for _, s := range args.Stoppables {
		err := s.Stop(ctx)
		if err != nil {
			log.Errorf("Error stopping service: %v", err)
		}
	}
}

func ProvideBaseContext() context.Context {
	return context.Background()
}

func ProvideConfig() *config.AppConfig {
	conf, err := config.GetConf()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	return conf
}

func ProvideErrorsChan() chan error {
	return make(chan error, 1)
}

func ProvideClickhouse(ctx context.Context, conf *config.AppConfig, m *metrics.Service, errCh chan error) (*clickhouse.Service, error) {
	return clickhouse.NewService(ctx, conf.CHConf, m, errCh)
}

func ProvideLogger(ch *clickhouse.Service) *log.Logger {
	logger := log.New()
	logger.SetLevel(log.DebugLevel)
	hook := clickhouse.NewLogHook(ch.Client)
	logger.AddHook(hook)
	return logger
}

func ProvideMetrics(conf *config.AppConfig) *metrics.Service {
	errCh := make(chan error, 1)
	svc := metrics.New(conf.Metrics)
	svc.Start(errCh)
	return svc
}

func ProvideDaemon(ctx context.Context, m *metrics.Service, ch *clickhouse.Service, logger *log.Logger) *daemon.Daemon {
	return daemon.New(ctx, numWorkers, queueSize, m, ch, logger)
}

func ProvideWebAPI(conf *config.AppConfig, d *daemon.Daemon, m *metrics.Service, logger *log.Logger) *webapi.API {
	return webapi.New(conf.WebAPI, d, m, logger)
}
