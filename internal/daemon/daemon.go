package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
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
	return fmt.Sprintf("task-%d", time.Now().UnixNano()+int64(rand.Intn(1000)))
}

type Daemon struct {
	logger       *log.Logger
	Metrics      *metrics.Metrics
	TaskQueue    chan *Task
	numWorkers   int
	Wg           *sync.WaitGroup
	workerCancel func()
	ExitCh       chan struct{}
}

func New(numWorkers int, queueSize int, m *metrics.Metrics, cancel context.CancelFunc, logger *log.Logger) *Daemon {
	return &Daemon{
		logger:       logger,
		Metrics:      m,
		TaskQueue:    make(chan *Task, queueSize),
		numWorkers:   numWorkers,
		Wg:           &sync.WaitGroup{},
		ExitCh:       make(chan struct{}, 1),
		workerCancel: cancel,
	}
}

func (d *Daemon) Start(ctx context.Context, apiCaller ExternalAPICaller) {
	for i := range d.numWorkers {
		id := i + 1
		go d.worker(ctx, apiCaller, id)
	}
}

func (d *Daemon) Stop() {
	defer close(d.ExitCh)

	doneCh := make(chan struct{})
	go func() {
		close(d.TaskQueue)
		d.Wg.Wait()
		close(doneCh)
	}()

	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		d.workerCancel()
		d.logger.Info("force exit after timeout")
	}

	formattedMetrics, err := json.MarshalIndent(d.Metrics.GetMetrics(), "", "  ")
	if err != nil {
		d.logger.WithError(err).Error("Failed to format metrics")
	}
	d.logger.Info("Metrics", "data", string(formattedMetrics))
	d.logger.Info("All workers have stopped")
}

func (d *Daemon) worker(ctx context.Context, apiCaller ExternalAPICaller, workerID int) {
	for {
		select {
		case <-ctx.Done():
			d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
			return
		case task, ok := <-d.TaskQueue:
			if !ok {
				d.logger.WithFields(log.Fields{"workerId": workerID}).Info("quit due to task queue closed")
				return
			}
			select {
			case <-ctx.Done():
				d.logger.WithFields(log.Fields{"workerId": workerID}).Info("stopped by context done")
				return
			default:
				d.Metrics.SetActiveTaskID(task.ID)
				d.Wg.Add(1)
				go d.processingWithTimeout(ctx, apiCaller, workerID, task)
			}

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
		finishedAt := time.Since(startedAt)
		d.Metrics.AddTaskDuration(finishedAt)
		d.Metrics.UnsetActiveTaskID(task.ID)
		d.logger.WithFields(log.Fields{"workerId": workerID, "taskId": task.ID, "duration": finishedAt}).Info("finished processing")
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
