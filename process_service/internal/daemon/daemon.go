package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	log "github.com/sirupsen/logrus"

	"process_service/extapi"
	"process_service/internal/bus"
	"process_service/internal/dlq"
	"process_service/internal/domain"
	"process_service/internal/metrics"
)

const (
	redisStreamName = "tasks"
	redisGroupName  = "task_group"
)

type ExternalAPICaller interface {
	GetSomething(ctx context.Context, taskID string, workerID int) error
}

type PersistentQueue interface {
	AddNotProcessedTask(taskID string)
	GetAllNotProcessedTasks() []string
}

type Daemon struct {
	baseCtx     context.Context
	logger      *log.Logger
	numWorkers  int
	taskCounter uint64
	consumer *bus.Consumer
	Metrics     *metrics.Service
	Q           PersistentQueue
	
	Sem         chan struct{}
	Wg          *sync.WaitGroup
	workerCancel func()
}

func New(ctx context.Context, conf *bus.Config, numWorkers int, queueSize int, m *metrics.Service, db PersistentQueue, statusHook bus.InvalidTaskStatusUpdater, logger *log.Logger) *Daemon {

	rdb := redis.NewClient(&redis.Options{Addr: conf.RedisAddr})

	var dlqWriter dlq.Writer
	if minioDLQ, err := dlq.NewMinIOFromEnv(); err != nil {
		logger.WithError(err).Warn("failed to initialize minio dlq storage")
	} else {
		dlqWriter = minioDLQ
	}

	if err := rdb.XGroupCreateMkStream(ctx, redisStreamName, redisGroupName, "$").Err(); err != nil && !errors.Is(err, redis.Nil) {
		if err.Error() != "BUSYGROUP Consumer Group name already exists" {
			logger.WithError(err).Warn("failed to ensure redis stream consumer group")
		}
	}

	return &Daemon{
		logger:      logger,
		Metrics:     m,
		consumer:    bus.NewConsumer(rdb, dlqWriter, statusHook),
		Sem:         make(chan struct{}, queueSize),
		numWorkers:  numWorkers,
		Wg:          &sync.WaitGroup{},
		baseCtx:     ctx,
		Q:           db,
	}
}

func (d *Daemon) Start(ctx context.Context, apiCaller ExternalAPICaller) {
	d.baseCtx = ctx
	workerCtx, cancel := context.WithCancel(ctx)
	d.workerCancel = cancel
	for i := 0; i < d.numWorkers; i++ {
		id := i + 1
		go d.worker(workerCtx, apiCaller, id)
	}
}

func (d *Daemon) Stop(_ context.Context) error {
	d.workerCancel()

	doneCh := make(chan struct{})
	go func() {
		d.Wg.Wait()
		close(doneCh)
	}()

	<-doneCh

	d.logger.Info("submitted tasks:", d.Metrics.Recorder.GetSubmittedTasksTotal())
	d.logger.Info("unavailable service:", d.Metrics.Recorder.GetUnavailableTotal())
	d.logger.Info("errors:", d.Metrics.Recorder.GetTaskErrorsTotal())
	d.logger.Info("timeouts:", d.Metrics.Recorder.GetTimeoutsTotal())
	d.logger.Info("active tasks:", d.Metrics.Recorder.GetActiveTasksTotal())
	d.logger.Info("All workers have stopped")
	d.logFinalMetrics()
	return nil
}

func (d *Daemon) worker(ctx context.Context, apiCaller ExternalAPICaller, workerID int) {
	for {
		select {
		case <-ctx.Done():
			d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
			return
		default:
			err := d.consumer.ConsumeTasks(ctx, apiCaller, workerID, d.handleTask)
			if err != nil {
				d.logger.WithFields(log.Fields{"workerId": workerID, "error": err}).Error("error consuming tasks")
			}
		}
	}
}

func (d *Daemon) handleTask(ctx context.Context, apiCaller bus.ExternalAPICaller, workerID int, task *domain.Task) error {
	d.Wg.Add(1)
	defer d.Wg.Done()

	processingCtx, cancel := context.WithTimeout(ctx, time.Duration(3)*time.Second)
	defer cancel()

	d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID.String()}).Info("start processing")
	d.Metrics.Recorder.AddActiveTasks(1)
	startedAt := time.Now()
	defer func() {
		d.Metrics.Recorder.ObserveTaskDuration(time.Since(startedAt))
		d.Metrics.Recorder.DecActiveTasks(1)
	}()

	err := apiCaller.GetSomething(processingCtx, task.ID.String(), workerID)
	if err != nil {
		var customErr *extapi.CustomError
		if errors.As(err, &customErr) {
			d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID.String(), "error": customErr.Msg}).Error("External API error")
			d.Metrics.Recorder.IncTaskError()
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			d.Metrics.Recorder.IncTaskTimeout()
		}
		d.Q.AddNotProcessedTask(task.ID.String())
		return err
	}

	d.Metrics.Recorder.IncProcessedTasks(true)
	return nil
}

func (d *Daemon) logFinalMetrics() {
	metrics := d.Metrics.Recorder.GetMetrics()
	metrics["not_processed_tasks_count"] = uint64(len(d.Q.GetAllNotProcessedTasks()))
	metrics["not_processed_tasks"] = d.Q.GetAllNotProcessedTasks()
	formattedMetrics, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		d.logger.WithError(err).Error("Failed to format metrics")
	}
	d.logger.Info(string(formattedMetrics))
}
