package main

import (
	"context"
	"fmt"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	log "github.com/sirupsen/logrus"

	"shortcut/extapi"
	"shortcut/internal/daemon"
	"shortcut/internal/metrics"
	webapi "shortcut/internal/web-api"
)

const (
	numWorkers = 5
	queueSize  = 100
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	extAPI := &extapi.ExternalAPIImplementation{}

	logger := log.New()

	m := metrics.New()
	d := daemon.New(numWorkers, queueSize, m, cancel, logger)
	d.Start(ctx, extAPI)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	api := webapi.New(d, m, logger)

	api.Start()

	go func() {
		for sig := range c {
			sigHandler(ctx, sig, d, api)
		}
	}()

	if _, ok := <-d.ExitCh; !ok {
		logger.Info("app exit")
		return
	}
}

func sigHandler(ctx context.Context, signal os.Signal, d *daemon.Daemon, api *webapi.API) {
	switch signal {
	case syscall.SIGTERM:
		d.Stop()
		api.Stop(ctx)
	case syscall.SIGINT:
		d.Stop()
		api.Stop(ctx)
	case syscall.SIGKILL:
		d.Stop()
		api.Stop(ctx)
	default:
		fmt.Printf("signal %s", signal.String())
	}
}
