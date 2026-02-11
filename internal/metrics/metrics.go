package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	_ "net/http/pprof"
)

const (
	statusCodeLabel = "code"
	methodLabel     = "method"
	errorLabel      = "error"
)

type RecorderConfig struct {
	Prefix          string    `mapstructure:"prefix"`
	DurationBuckets []float64 `mapstructure:"duration_buckets"`
	SizeBuckets     []float64 `mapstructure:"size_buckets"`
}

// Recorder contains prometheus metrics used in app
type Recorder struct {
	conf *RecorderConfig

	statusCounter *prometheus.CounterVec // 200, 503
	errorCounter  *prometheus.CounterVec //timeouts, common errors

	taskDuration prometheus.Histogram

	memUsed              prometheus.Gauge
	activeTasks          prometheus.Gauge
	httpRequestsInflight prometheus.Gauge
}

// NewRecorder returns a new metrics recorder that implements the recorder
// using Prometheus as the backend.
func NewRecorder() *Recorder {
	conf := &RecorderConfig{
		DurationBuckets: prometheus.DefBuckets,
		SizeBuckets:     prometheus.ExponentialBuckets(100, 10, 8),
	}

	r := &Recorder{
		conf: conf,

		statusCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: conf.Prefix,
			Subsystem: "http",
			Name:      "requests_accepted_total",
			Help:      "The total number of accepted HTTP requests.",
		}, []string{statusCodeLabel}),

		errorCounter: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: conf.Prefix,
			Subsystem: "task",
			Name:      "errors_total",
			Help:      "The total number of task errors.",
		}, []string{errorLabel}),

		taskDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: conf.Prefix,
			Subsystem: "task",
			Name:      "duration_seconds",
			Help:      "The duration of task processing in seconds.",
			Buckets:   conf.DurationBuckets,
		}),

		memUsed: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: conf.Prefix,
			Subsystem: "http",
			Name:      "mem_used_bytes",
			Help:      "The number of bytes of memory used.",
		}),
		activeTasks: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: conf.Prefix,
			Subsystem: "task",
			Name:      "active_tasks",
			Help:      "The number of active tasks being processed at the same time.",
		}),
		httpRequestsInflight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: conf.Prefix,
			Subsystem: "http",
			Name:      "requests_inflight",
			Help:      "The number of inflight requests being handled at the same time.",
		}),
	}

	return r
}

func (r *Recorder) GetMetrics() map[string]any {
	metrics := make(map[string]any)
	metrics["mem_used_bytes"] = r.GetMemUsed()
	metrics["active_tasks"] = r.GetActiveTasksTotal()
	metrics["unavailable_total"] = r.GetUnavailableTotal()
	metrics["submitted_tasks_total"] = r.GetSubmittedTasksTotal()
	metrics["task_errors_total"] = r.GetTaskErrorsTotal()
	metrics["timeouts_total"] = r.GetTimeoutsTotal()
	return metrics
}

func (r *Recorder) GetMemUsed() float64 {
	metric := &dto.Metric{}
	if err := r.memUsed.Write(metric); err != nil {
		return 0
	}
	return metric.GetGauge().GetValue()
}

func (r *Recorder) GetActiveTasksTotal() uint64 {
	metric := &dto.Metric{}
	if err := r.activeTasks.Write(metric); err != nil {
		return 0
	}
	return uint64(metric.GetGauge().GetValue())
}

func (r *Recorder) GetUnavailableTotal() uint64 {
	metric := &dto.Metric{}
	if err := r.statusCounter.WithLabelValues("503").Write(metric); err != nil {
		return 0
	}
	return uint64(metric.GetCounter().GetValue())
}

func (r *Recorder) GetSubmittedTasksTotal() uint64 {
	metric := &dto.Metric{}
	if err := r.statusCounter.WithLabelValues("200").Write(metric); err != nil {
		return 0
	}
	return uint64(metric.GetCounter().GetValue())
}

func (r *Recorder) GetTaskErrorsTotal() uint64 {
	metric := &dto.Metric{}
	if err := r.errorCounter.WithLabelValues("error").Write(metric); err != nil {
		return 0
	}
	return uint64(metric.GetCounter().GetValue())
}

func (r *Recorder) GetTimeoutsTotal() uint64 {
	metric := &dto.Metric{}
	if err := r.errorCounter.WithLabelValues("timeout").Write(metric); err != nil {
		return 0
	}
	return uint64(metric.GetCounter().GetValue())
}

func (r *Recorder) IncHTTPResponseStatus(statusCode int) {
	r.statusCounter.WithLabelValues(strconv.Itoa(statusCode)).Inc()
}

func (r *Recorder) IncSubmittedTasksTotal() {
	r.statusCounter.WithLabelValues("200").Inc()
}

func (r *Recorder) IncTaskError() {
	r.errorCounter.WithLabelValues("error").Inc()
}

func (r *Recorder) IncTaskTimeout() {
	r.errorCounter.WithLabelValues("timeout").Inc()
}

func (r *Recorder) AddActiveTasks(count float64) {
	r.activeTasks.Add(float64(count))
}

func (r *Recorder) DecActiveTasks(count float64) {
	r.activeTasks.Sub(count)
}

// ObserveTaskDuration updates httpRequestDurHistogram metric with passed request
func (r *Recorder) ObserveTaskDuration(duration time.Duration) {
	r.taskDuration.
		Observe(duration.Seconds())
}

// AddInflightRequests updates httpRequestsInflight metric with passed request
func (r *Recorder) AddInflightRequests(quantity int) {
	r.httpRequestsInflight.Add(float64(quantity))
}

// Service struct
type Service struct {
	API      *API
	Recorder *Recorder
}

// New constructor
func New(conf *Config) *Service {
	return &Service{
		API:      newAPI(conf),
		Recorder: NewRecorder(),
	}
}

// Start registers metrics and starts the metrics HTTP API.
func (s *Service) Start() error {
	if err := s.Recorder.RegisterMetrics(); err != nil {
		return err
	}
	if s.API != nil {
		s.API.Start()
	}
	return nil
}

// Stop stops the metrics HTTP API.
func (s *Service) Stop() error {
	if s.API != nil {
		return s.API.Stop()
	}
	return nil
}

// RegisterMetrics registers needed metrics with default prometheus registerer
func (r *Recorder) RegisterMetrics() error {
	metricsToRegister := []prometheus.Collector{
		r.activeTasks, r.errorCounter, r.taskDuration, r.memUsed, r.httpRequestsInflight, r.statusCounter,
	}

	for _, metric := range metricsToRegister {
		if err := prometheus.DefaultRegisterer.Register(metric); err != nil {
			return err
		}
	}

	return nil
}
