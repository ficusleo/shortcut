package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"submit_service/internal/bus"
	"submit_service/internal/domain"
	"submit_service/internal/metrics"
	"submit_service/internal/services"
)

const (
	redisStreamName = "tasks"
	redisGroupName  = "task_group"
)

type Daemon struct {
	baseCtx     context.Context
	logger      *log.Logger
	taskSrv *services.TaskService
	numWorkers  int
	taskCounter uint64
	redisSrv *bus.Service
	Metrics     *metrics.Service
	
	Sem         chan struct{}
	Wg          *sync.WaitGroup
	workerCancel func()
}

func New(ctx context.Context, srv *bus.Service, numWorkers int, queueSize int, m *metrics.Service, logger *log.Logger) *Daemon {
	return &Daemon{
		logger:      logger,
		Metrics:     m,
		redisSrv:    srv,
		Sem:         make(chan struct{}, queueSize),
		numWorkers:  numWorkers,
		Wg:          &sync.WaitGroup{},
		baseCtx:     ctx,
	}
}

func (d *Daemon) Start(ctx context.Context) {
	d.baseCtx = ctx
	workerCtx, cancel := context.WithCancel(ctx)
	d.workerCancel = cancel
	for i := 0; i < d.numWorkers; i++ {
		id := i + 1
		go d.worker(workerCtx, id)
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

func (d *Daemon) worker(ctx context.Context, workerID int) {
	for {
		select {
		case <-ctx.Done():
			d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
			return
		default:
			d.redisSrv.Consumer.ConsumeTasks(ctx, workerID, d.process)
		}
	}
}


func (d *Daemon) process(ctx context.Context, workerID int, task *domain.Task) error {
	defer d.Wg.Done()
	processingCtx, cancel := context.WithTimeout(ctx, time.Duration(3)*time.Second)
	defer cancel()

	d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID}).Info("start processing")

	doneProcessing := make(chan struct{})
	errChan := make(chan error, 1)
	go func() {
		if task.Status == domain.StatusFailed{
			err := d.taskSrv.UpdateTaskStatus(task.ID, domain.StatusFailed)
			if err != nil {
				errChan <- err
				return
			}

		}
		close(doneProcessing)
	}()

	select {
	case <-processingCtx.Done():
		if processingCtx.Err() == context.DeadlineExceeded {
			d.Metrics.Recorder.IncTaskTimeout()
			d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID}).Warn("task processing timed out")
		}
		return nil
	case err := <-errChan:
		return err
	case <-doneProcessing:
		d.Metrics.Recorder.IncProcessedTasks(true)
		return nil
	}
}

func (d *Daemon) logFinalMetrics() {
	metrics := d.Metrics.Recorder.GetMetrics()
	formattedMetrics, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		d.logger.WithError(err).Error("Failed to format metrics")
	}
	d.logger.Info(string(formattedMetrics))
}
