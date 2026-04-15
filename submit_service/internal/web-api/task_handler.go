package webapi

import (
	"context"
	"net/http"

	"submit_service/internal/domain"
	"submit_service/internal/metrics"
	"submit_service/internal/services"

	"github.com/google/uuid"
)

type TaskBus interface {
	ProduceTask(ctx context.Context, task *domain.Task) error
}

type TaskHandler struct {
	bus    TaskBus
	taskService *services.TaskService
	sem         chan struct{}
	metrics     *metrics.Service
}

func NewTaskHandler(taskService *services.TaskService, taskBus TaskBus, m *metrics.Service) *TaskHandler {
	return &TaskHandler{
		bus:         taskBus,
		taskService: taskService,
		sem:         make(chan struct{}, 100), // Ограничение на 100 одновременных задач
		metrics:     m,
	}
}

func (th *TaskHandler) SubmitTask(w http.ResponseWriter, r *http.Request) {
	if isShuttingDown.Load() {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	
	status := http.StatusAccepted

	select {
	case th.sem <- struct{}{}:
		taskStatus := domain.StatusProcessing
		payload := r.FormValue("payload")
		if payload == "" {
			http.Error(w, "Payload is required", http.StatusBadRequest)
			th.metrics.Recorder.IncHTTPResponseStatus(http.StatusBadRequest)
			return
		}
		th.startTaskProcessing(r.Context(), w, &domain.Task{
			ID: uuid.New(), Status: taskStatus, Payload: &payload,
		})
	default:
		status = http.StatusServiceUnavailable
		http.Error(w, "Task queue is full, try again later", status)
		th.metrics.Recorder.IncHTTPResponseStatus(status)
		return
	}
	th.metrics.Recorder.IncHTTPResponseStatus(status)

	w.WriteHeader(status)
}

func (th *TaskHandler) startTaskProcessing(ctx context.Context, w http.ResponseWriter, task *domain.Task) {
	defer func() { <-th.sem }()
	if err := th.taskService.InsertTask(task); err != nil {
		http.Error(w, "Failed to insert task", http.StatusInternalServerError)
		th.metrics.Recorder.IncHTTPResponseStatus(http.StatusInternalServerError)
		return
	}
	if err := th.bus.ProduceTask(ctx, task); err != nil {
		if err := th.taskService.UpdateTaskStatus(task.ID, domain.StatusPending); err != nil {
			http.Error(w, "Failed to update task status to failed", http.StatusInternalServerError)
			th.metrics.Recorder.IncHTTPResponseStatus(http.StatusInternalServerError)
		}
		http.Error(w, "Failed to produce task", http.StatusInternalServerError)
		th.metrics.Recorder.IncHTTPResponseStatus(http.StatusInternalServerError)
		return
	}
	
}

func (th *TaskHandler) ResumeTask(w http.ResponseWriter, r *http.Request) {
	if isShuttingDown.Load() {
		http.Error(w, "shutting down", http.StatusServiceUnavailable)
		return
	}
	taskIDStr := r.URL.Query().Get("id")
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		http.Error(w, "Invalid task ID", http.StatusBadRequest)
		return
	}
	task, err := th.taskService.GetTaskByID(taskID)
	if err != nil {
		http.Error(w, "Failed to get task", http.StatusInternalServerError)
		return
	}
	if task == nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	switch task.Status {
	case domain.StatusFailed, domain.StatusPending:
		if err := th.taskService.UpdateTaskStatus(task.ID, domain.StatusPending); err != nil {
			http.Error(w, "Failed to update task status", http.StatusInternalServerError)
			return
		}
		th.startTaskProcessing(r.Context(), w, task)
		w.WriteHeader(http.StatusAccepted)
	case domain.StatusProcessing:
		http.Error(w, "Task is already in progress or pending", http.StatusBadRequest)
		return
	case domain.StatusProcessed:
		http.Error(w, "Task is already processed", http.StatusBadRequest)
		return
	default:
		http.Error(w, "Only failed tasks can be resumed", http.StatusBadRequest)
		return
	}
}
