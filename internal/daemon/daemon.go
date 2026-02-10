package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"

	"shortcut/extapi"
	"shortcut/internal/database"
	"shortcut/internal/metrics"
)

type ExternalAPICaller interface {
	GetSomething(ctx context.Context, taskID string, workerID int) error
}

type PersistentQueue interface {
	AddNotProcessedTask(taskID string)
	GetAllNotProcessedTasks() []string
}

type Task struct {
	ID string
}

// Генерируем простой ID
func (d *Daemon) NewTaskID() string {
	nextID := atomic.AddUint64(&d.taskCounter, 1)
	return fmt.Sprintf("task-%d", nextID)
}

type Daemon struct {
	logger      *log.Logger
	baseCtx     context.Context
	numWorkers  int
	taskCounter uint64
	Metrics     *metrics.Service
	TaskQueue   chan *Task
	Wg          *sync.WaitGroup
	Ch          PersistentQueue

	workerCancel func()
}

func New(ctx context.Context, numWorkers int, queueSize int, m *metrics.Service, db *database.Service, logger *log.Logger) *Daemon {
	return &Daemon{
		logger:     logger,
		Metrics:    m,
		TaskQueue:  make(chan *Task, queueSize),
		numWorkers: numWorkers,
		Wg:         &sync.WaitGroup{},
		baseCtx:    ctx,
		Ch:         db,
	}
}

func (d *Daemon) Start(ctx context.Context, apiCaller ExternalAPICaller) {
	d.baseCtx = ctx
	ctx, d.workerCancel = context.WithCancel(ctx)
	for i := range d.numWorkers {
		id := i + 1
		go d.worker(ctx, apiCaller, id)
	}
}

func (d *Daemon) Stop() {
	d.workerCancel()

	close(d.TaskQueue)
	d.moveNotProcessedTasksToPersistentQueue()

	doneCh := make(chan struct{})
	go func() {
		d.Wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// all active tasks finished processing
	case <-time.After(10 * time.Minute):
		d.logger.Info("force exit after timeout")
	}

	d.logger.Info("submitted tasks:", d.Metrics.Recorder.GetSubmittedTasksTotal())
	d.logger.Info("unavailable service:", d.Metrics.Recorder.GetUnavailableTotal())
	d.logger.Info("errors:", d.Metrics.Recorder.GetTaskErrorsTotal())
	d.logger.Info("timeouts:", d.Metrics.Recorder.GetTimeoutsTotal())
	d.logger.Info("active tasks:", d.Metrics.Recorder.GetActiveTasksTotal())
	d.logger.Info("All workers have stopped")
	d.logFinalMetrics()
}

func (d *Daemon) worker(ctx context.Context, apiCaller ExternalAPICaller, workerID int) {
	for {
		select {
		case <-ctx.Done():
			d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
			return
		case task, ok := <-d.TaskQueue:
			if !ok {
				d.logger.WithFields(log.Fields{"workerId": workerID}).Info("task queue closed, worker exiting")
				return
			}
			d.Metrics.Recorder.AddActiveTasks(1)
			d.Wg.Add(1)
			d.processingWithTimeout(d.baseCtx, apiCaller, workerID, task)
		}
	}
}

func (d *Daemon) processingWithTimeout(ctx context.Context, apiCaller ExternalAPICaller, workerID int, task *Task) {
	defer d.Wg.Done()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(3)*time.Second)
	defer cancel()

	d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID}).Info("start processing")
	startedAt := time.Now()
	defer func() {
		d.Metrics.Recorder.ObserveTaskDuration(time.Since(startedAt))
		d.Metrics.Recorder.DecActiveTasks(1)
	}()

	doneProcessing := make(chan struct{})
	errChan := make(chan error, 1)
	go func() {
		err := apiCaller.GetSomething(ctx, task.ID, workerID)
		if err != nil {
			var customErr *extapi.CustomError
			if errors.As(err, &customErr) {
				d.Metrics.Recorder.IncTaskError()
			}
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				d.Metrics.Recorder.IncTaskTimeout()
			}
			errChan <- err
			return
		}
		d.Metrics.Recorder.IncSubmittedTasksTotal()
		close(doneProcessing)
	}()

	select {
	case <-errChan:
		return
	case <-doneProcessing:
		return
	}
}
func (d *Daemon) moveNotProcessedTasksToPersistentQueue() {
	for task := range d.TaskQueue {
		d.Ch.AddNotProcessedTask(task.ID)
	}
}

func (d *Daemon) logFinalMetrics() {
	metrics := d.Metrics.Recorder.GetMetrics()
	metrics["not_processed_tasks_count"] = uint64(len(d.Ch.GetAllNotProcessedTasks()))
	metrics["not_processed_tasks"] = d.Ch.GetAllNotProcessedTasks()
	formattedMetrics, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		d.logger.WithError(err).Error("Failed to format metrics")
	}
	d.logger.Info(string(formattedMetrics))
}
