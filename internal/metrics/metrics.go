package metrics

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	_ "net/http/pprof"
)

type MetricsResponse struct {
	TasksSubmitted    uint64
	TaskMeanDuration  string
	ActiveTaskIDs     []string
	TaskErrorsTotal   uint64
	TaskTimeoutsTotal uint64
}

type Metrics struct {
	Mux             *sync.RWMutex
	TaskErrorsTotal uint64
	TimeoutsTotal   uint64
	ActiveTaskIDs   map[string]struct{}
	TaskDurations   []time.Duration
	Submitted       uint64
}

func New() *Metrics {
	return &Metrics{
		Mux:           &sync.RWMutex{},
		ActiveTaskIDs: make(map[string]struct{}),
		TaskDurations: make([]time.Duration, 0),
	}
}

func (m *Metrics) GetMetrics() *MetricsResponse {
	count := atomic.LoadUint64(&m.Submitted)
	return &MetricsResponse{
		TasksSubmitted:    count,
		TaskMeanDuration:  m.GetTaskMeanDuration(),
		ActiveTaskIDs:     m.GetActiveTaskIDs(),
		TaskErrorsTotal:   m.GetTaskErrorsTotal(),
		TaskTimeoutsTotal: m.GetTaskTimeoutsTotal(),
	}
}

func (m *Metrics) MetricsHandler(w http.ResponseWriter, _ *http.Request) {
	resp := m.GetMetrics()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	formattedResponse, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		http.Error(w, "Failed to format response", http.StatusInternalServerError)
		return
	}
	w.Write(formattedResponse)
}

func (m *Metrics) SetActiveTaskID(taskID string) {
	m.Mux.Lock()
	defer m.Mux.Unlock()
	m.ActiveTaskIDs[taskID] = struct{}{}
}

func (m *Metrics) UnsetActiveTaskID(taskID string) {
	m.Mux.Lock()
	defer m.Mux.Unlock()
	delete(m.ActiveTaskIDs, taskID)
}

func (m *Metrics) GetTaskErrorsTotal() uint64 {
	return atomic.LoadUint64(&m.TaskErrorsTotal)
}

func (m *Metrics) GetTaskTimeoutsTotal() uint64 {
	return atomic.LoadUint64(&m.TimeoutsTotal)
}

func (m *Metrics) GetActiveTaskIDs() []string {
	res := make([]string, 0, len(m.ActiveTaskIDs))
	for id := range m.ActiveTaskIDs {
		res = append(res, id)
	}
	return res
}

func (m *Metrics) AddTaskDuration(duration time.Duration) {
	m.Mux.Lock()
	defer m.Mux.Unlock()
	m.TaskDurations = append(m.TaskDurations, duration)
}

func (m *Metrics) GetTaskMeanDuration() string {
	if len(m.TaskDurations) == 0 {
		return "0s"
	}
	m.Mux.RLock()
	defer m.Mux.RUnlock()
	var total uint64
	for _, d := range m.TaskDurations {
		total += uint64(d)
	}
	return time.Duration(float64(total) / float64(len(m.TaskDurations))).String()
}
