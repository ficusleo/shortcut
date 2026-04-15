package main

import (
	"context"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"
	"go.uber.org/dig"

	"submit_service/internal/bus"
	"submit_service/internal/config"
	"submit_service/internal/metrics"
	"submit_service/internal/repository"
	"submit_service/internal/services"
	webapi "submit_service/internal/web-api"
)

const (
	numWorkers       = 5
	queueSize        = 100
	_shutdownTimeout = 15 * time.Second
)

func main() {
	container := dig.New()

	container.Provide(ProvideConfig)
	container.Provide(ProvideErrorsChan)
	container.Provide(ProvideBaseContext)
	container.Provide(ProvideLogger)
	container.Provide(ProvideRepository)
	container.Provide(ProvideRedisClient)
	container.Provide(ProvideTaskProducer)
	container.Provide(ProvideTaskService)
	container.Provide(ProvideMetrics)
	container.Provide(ProvideWebAPI)

	// the dependencies will stop in the order they were registered in the stoppables group
	// should stop them in this order to ensure no data loss:
	// webapi.API: Stop receiving new traffic (using the readiness logic we just added).
	// metrics.Service: Stop the metrics server only after everything else is done.
	container.Provide(func(api *webapi.API) Stoppable { return api }, dig.Group("stoppables"))
	container.Provide(func(m *metrics.Service) Stoppable { return m }, dig.Group("stoppables"))

	if err := container.Invoke(func(ctx context.Context, args RunArgs) {
		defer stop(ctx, args.Stop)

		args.Repo.Start()
		args.API.Start()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		for {
			select {
			case <-sigCh:
				log.Info("Received shutdown signal, exiting...")
				return
			case err := <-args.Repo.ErrCh:
				log.Errorf("repository error: %v", err)
				return
			}
		}
	}); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

}

type RunArgs struct {
	dig.In
	Repo   *repository.Service
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
	select {
	// all active tasks finished processing
	case <-time.After(_shutdownTimeout):
		log.Info("Shutdown timed out, exiting immediately")
	default:
		for _, s := range args.Stoppables {
			err := s.Stop(ctx)
			if err != nil {
				log.Errorf("Error stopping service: %v", err)
			}
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

func ProvideRepository(ctx context.Context, conf *config.AppConfig, m *metrics.Service, errCh chan error) (*repository.Service, error) {
	return repository.NewService(ctx, conf.RepoConf, m, errCh)
}

func ProvideLogger(ch *repository.Service) *log.Logger {
	logger := log.New()
	logger.SetLevel(log.DebugLevel)
	hook := repository.NewLogHook(ch.Client)
	logger.AddHook(hook)
	return logger
}

func ProvideMetrics(conf *config.AppConfig) *metrics.Service {
	errCh := make(chan error, 1)
	svc := metrics.New(conf.Metrics)
	svc.Start(errCh)
	return svc
}

func ProvideRedisClient(conf *config.AppConfig) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: conf.RedisConf.RedisAddr})
}

func ProvideTaskProducer(redisClient *redis.Client) *bus.Producer {
	return bus.NewProducer(redisClient)
}

func ProvideTaskService(repo *repository.Service) *services.TaskService {
	return services.NewTaskService(repository.NewTaskRepository(repo))
}

func ProvideWebAPI(ctx context.Context, conf *config.AppConfig, taskSrv *services.TaskService, producer *bus.Producer, m *metrics.Service, logger *log.Logger) *webapi.API {
	return webapi.New(ctx, conf.WebAPI, taskSrv, producer, m, logger)
}
