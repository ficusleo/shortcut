package webapi

import (
	"encoding/json"
	"math"
	"net/http"
	"runtime"
	"sync"
	"time"
)

type CPULoadHandler struct {
}

func NewCPULoadHandler() *CPULoadHandler {
	return &CPULoadHandler{}
}

func (h *CPULoadHandler) CPULoadHandler(w http.ResponseWriter, r *http.Request) {
	workers, err := parsePositiveInt(r.URL.Query().Get("workers"), runtime.NumCPU())
	if err != nil {
		http.Error(w, "workers must be a positive integer", http.StatusBadRequest)
		return
	}
	seconds, err := parsePositiveInt(r.URL.Query().Get("seconds"), 30)
	if err != nil {
		http.Error(w, "seconds must be a positive integer", http.StatusBadRequest)
		return
	}

	go runCPULoad(workers, time.Duration(seconds)*time.Second)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "cpu load started",
		"workers": workers,
		"seconds": seconds,
	})
}

func runCPULoad(workers int, duration time.Duration) {
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	wg.Add(workers)

	for i := range workers {
		go func(offset int) {
			defer wg.Done()
			value := float64(offset + 1)
			for time.Now().Before(deadline) {
				value = math.Sqrt(value*1.000001 + 123.456)
				if value > 100000 {
					value = 1
				}
			}
		}(i)
	}

	wg.Wait()
}
