package webapi

import (
	"encoding/json"
	"net/http"
	"submit_service/internal/metrics"
	"submit_service/internal/services"
)

type MetricsHandler struct {
	taskService *services.TaskService
	metrics     *metrics.Service
}

func NewMetricsHandler(taskSrv *services.TaskService, m *metrics.Service) *MetricsHandler {
	return &MetricsHandler{
		taskService: taskSrv,
		metrics:     m,
	}
}

func (mh *MetricsHandler) LogMetrics(w http.ResponseWriter, r *http.Request) {
	resp := mh.metrics.Recorder.GetMetrics()
	tasks, err := mh.taskService.GetAllNotProcessedTasks()
	if err != nil {
		http.Error(w, "Failed to get not processed tasks", http.StatusInternalServerError)
		return
	}
	resp["not_processed_tasks_count"] = uint64(len(tasks))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	formattedResponse, err := json.MarshalIndent(resp, "", "  ")
	status := http.StatusAccepted
	if err != nil {
		status = http.StatusInternalServerError
		http.Error(w, "Failed to format response", status)
		return
	}
	w.Write(formattedResponse)
}
