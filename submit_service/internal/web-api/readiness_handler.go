package webapi

import (
	"net/http"
	"sync/atomic"
)

var isShuttingDown atomic.Bool

type ReadinessHandler struct {
}

func NewReadinessHandler() *ReadinessHandler {
	return &ReadinessHandler{}
}

func (rh *ReadinessHandler) HandleReadiness(w http.ResponseWriter, r *http.Request) {
	if isShuttingDown.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("shutting down"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
