package metrics

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	Addr     string `mapstructure:"addr"`
	Endpoint string `mapstructure:"endpoint"`
}

// API contains settings for the metrics api
type API struct {
	conf   *Config
	server *http.Server
}

func newRoutes(endpoint string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(endpoint, promhttp.Handler())
	return mux
}

func newAPI(conf *Config) *API {
	routes := newRoutes(conf.Endpoint)

	server := &http.Server{
		Addr:              conf.Addr,
		Handler:           routes,
		ReadHeaderTimeout: 0,
	}

	return &API{
		conf:   conf,
		server: server,
	}
}

// Start launches the metrics HTTP server in a goroutine.
func (a *API) Start(errCh chan error) {
	go func() {
		if err := a.server.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()
}

// Stop gracefully shuts down the metrics server.
func (a *API) Stop(ctx context.Context) error {
	return a.server.Shutdown(ctx)
}
