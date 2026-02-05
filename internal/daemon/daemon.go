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
	"shortcut/internal/metrics"
)

type ExternalAPICaller interface {
	GetSomething(ctx context.Context, workerID int) error
	SetTaskID(taskID string)
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
	logger       *log.Logger
	taskCounter  uint64
	Metrics      *metrics.Metrics
	TaskQueue    chan *Task
	numWorkers   int
	Wg           *sync.WaitGroup
	semaphore    chan struct{}
	workerCancel func()
}

func New(numWorkers int, queueSize int, m *metrics.Metrics, logger *log.Logger) *Daemon {
	return &Daemon{
		logger:     logger,
		Metrics:    m,
		TaskQueue:  make(chan *Task, queueSize),
		numWorkers: numWorkers,
		Wg:         &sync.WaitGroup{},
		semaphore:  make(chan struct{}, numWorkers),
	}
}

func (d *Daemon) Start(ctx context.Context, apiCaller ExternalAPICaller) {
	ctx, d.workerCancel = context.WithCancel(ctx)
	for i := range d.numWorkers {
		id := i + 1
		go d.worker(ctx, apiCaller, id)
	}
}

func (d *Daemon) Stop() {
	doneCh := make(chan struct{})
	go func() {
		defer close(d.TaskQueue)
		d.Wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
		// this mean all workers done their jobs either naturally and all of them hit d.Wg.Done()
		// or by context with timeout cancellation. We unblock select with recieving from doneCh and continue to print metrics
	case <-time.After(10 * time.Second):
		// if workers not done by 10 seconds we force cancel them. All workers inside have timeout handling
		// so they will exit gracefully by themselves after timeout and this case just for safety IN THEORY
		d.workerCancel()
		d.logger.Info("force exit after timeout")
	}

	formattedMetrics, err := json.MarshalIndent(d.Metrics.GetMetrics(), "", "  ")
	if err != nil {
		d.logger.WithError(err).Error("Failed to format metrics")
	}
	d.logger.Info(string(formattedMetrics))
	d.logger.Info("All workers have stopped")
}

func (d *Daemon) worker(ctx context.Context, apiCaller ExternalAPICaller, workerID int) {
	select {
	case <-ctx.Done():
		d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
		return
	default:
		for task := range d.TaskQueue {
			select {
			case <-ctx.Done():
				d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
				return
			default:
				d.Metrics.SetActiveTaskID(task.ID)
				d.Wg.Add(1)
				d.semaphore <- struct{}{}
				go d.processingWithTimeout(ctx, apiCaller, workerID, task)
			}
		}
	}
}

func (d *Daemon) processingWithTimeout(ctx context.Context, apiCaller ExternalAPICaller, workerID int, task *Task) {
	defer d.Wg.Done()
	defer func() { <-d.semaphore }()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(3)*time.Second)
	defer cancel()

	d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID}).Info("start processing")
	startedAt := time.Now()
	defer func() {
		finishedAt := time.Since(startedAt)
		d.Metrics.AddTaskDuration(finishedAt)
		d.Metrics.UnsetActiveTaskID(task.ID)
	}()

	doneProcessing := make(chan struct{})
	errChan := make(chan error, 1)
	go func() {
		apiCaller.SetTaskID(task.ID)
		err := apiCaller.GetSomething(ctx, workerID)
		if err != nil {
			var customErr *extapi.CustomError
			if errors.As(err, &customErr) {
				d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID, "error": err}).Warn("custom error occurred")
				atomic.AddUint64(&d.Metrics.TaskErrorsTotal, 1)
			}
			var timeoutErr = context.DeadlineExceeded
			if errors.Is(err, timeoutErr) {
				d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID, "error": err}).Warn("timeout occurred")
				atomic.AddUint64(&d.Metrics.TimeoutsTotal, 1)
			}
			errChan <- err
			return
		}
		atomic.AddUint64(&d.Metrics.Submitted, 1)
		close(doneProcessing)
	}()

	select {
	case <-ctx.Done():
		return
	case <-errChan:
		return
	case <-doneProcessing:
		return
	}
}
