package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"time"
)

type MemoryLoadHandler struct {
}

func NewMemoryLoadHandler() *MemoryLoadHandler {
	return &MemoryLoadHandler{}
}

func (h *MemoryLoadHandler) MemoryLoadHandler(w http.ResponseWriter, r *http.Request) {
	megabytes, err := parsePositiveInt(r.URL.Query().Get("mb"), 128)
	if err != nil {
		http.Error(w, "mb must be a positive integer", http.StatusBadRequest)
		return
	}
	seconds, err := parsePositiveInt(r.URL.Query().Get("seconds"), 30)
	if err != nil {
		http.Error(w, "seconds must be a positive integer", http.StatusBadRequest)
		return
	}

	go runMemoryLoad(megabytes, time.Duration(seconds)*time.Second)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "memory load started",
		"mb":      megabytes,
		"seconds": seconds,
	})
}

func runMemoryLoad(megabytes int, duration time.Duration) {
	chunk := make([]byte, megabytes*1024*1024)
	for i := 0; i < len(chunk); i += 4096 {
		chunk[i] = byte(i)
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(duration)
	for {
		select {
		case <-timeout:
			runtime.KeepAlive(chunk)
			return
		case <-ticker.C:
			for i := 0; i < len(chunk); i += 4096 {
				chunk[i]++
			}
		}
	}
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("value must be a positive integer")
	}
	return v, nil
}
