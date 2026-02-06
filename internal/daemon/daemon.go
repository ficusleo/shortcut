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
	GetSomething(ctx context.Context, taskID string, workerID int) error
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
	Metrics     *metrics.Metrics
	TaskQueue   chan *Task
	Wg          *sync.WaitGroup

	mu                     sync.Mutex
	submittedTasks         map[string]int
	activeTasks            map[string]int
	notProcessedTasks      map[string]struct{}
	notProcessedTasksCount int

	workerCancel func()
}

func New(ctx context.Context, numWorkers int, queueSize int, m *metrics.Metrics, logger *log.Logger) *Daemon {
	return &Daemon{
		logger:     logger,
		Metrics:    m,
		TaskQueue:  make(chan *Task, queueSize),
		numWorkers: numWorkers,
		Wg:         &sync.WaitGroup{},
		baseCtx:    ctx,

		submittedTasks:    make(map[string]int),
		activeTasks:       make(map[string]int),
		notProcessedTasks: make(map[string]struct{}),
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
	case <-time.After(10 * time.Second):
		d.logger.Info("force exit after timeout")
	}

	d.logger.Info("submitted tasks:", d.submittedTasks)
	d.logger.Info("not processed tasks:", d.notProcessedTasks)
	d.logger.Info("active tasks:", d.activeTasks)
	d.logger.Info("not processed tasks count:", d.getNotProcessedTasksCount())
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
			d.Metrics.SetActiveTaskID(task.ID, workerID)
			d.addActiveTask(task.ID, workerID)
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
		finishedAt := time.Since(startedAt)
		d.Metrics.AddTaskDuration(finishedAt)
		d.Metrics.UnsetActiveTaskID(task.ID)
		d.removeActiveTask(task.ID)
	}()

	doneProcessing := make(chan struct{})
	errChan := make(chan error, 1)
	go func() {
		err := apiCaller.GetSomething(ctx, task.ID, workerID)
		if err != nil {
			var customErr *extapi.CustomError
			if errors.As(err, &customErr) {
				atomic.AddUint64(&d.Metrics.TaskErrorsTotal, 1)
			}
			var timeoutErr = context.DeadlineExceeded
			if errors.Is(err, timeoutErr) {
				atomic.AddUint64(&d.Metrics.TimeoutsTotal, 1)
			}
			var cancelErr = context.Canceled
			if errors.Is(err, cancelErr) {
				atomic.AddUint64(&d.Metrics.TimeoutsTotal, 1)
			}
			errChan <- err
			return
		}
		atomic.AddUint64(&d.Metrics.Submitted, 1)
		d.addSubmittedTask(task.ID, workerID)
		close(doneProcessing)
	}()

	select {
	case <-errChan:
		return
	case <-doneProcessing:
		return
	}
}

func (d *Daemon) addActiveTask(taskID string, workerID int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeTasks[taskID] = workerID
}

func (d *Daemon) removeActiveTask(taskID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.activeTasks, taskID)
}

func (d *Daemon) addNotProcessedTask(taskID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.notProcessedTasks[taskID] = struct{}{}
}

func (d *Daemon) moveNotProcessedTasksToPersistentQueue() {
	for task := range d.TaskQueue {
		d.addNotProcessedTask(task.ID)
	}
}

func (d *Daemon) addSubmittedTask(taskID string, workerID int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.submittedTasks[taskID] = workerID
}

func (d *Daemon) logFinalMetrics() {
	formattedMetrics, err := json.MarshalIndent(d.Metrics.GetMetrics(), "", "  ")
	if err != nil {
		d.logger.WithError(err).Error("Failed to format metrics")
	}
	d.logger.Info(string(formattedMetrics))
}

func (d *Daemon) getNotProcessedTasksCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.notProcessedTasks)
}
