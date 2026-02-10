package metrics

import (
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
