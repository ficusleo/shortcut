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

// first we recieve signal
// then we send to exitCh
// then by recieving from exitCh return from main
// by returning from main we call defer cancel() which cancels context
// then in daemon we listen for ctx.Done() and stop workers gracefully

func main() {
	ctx := context.Background()
	exitCh := make(chan struct{}, 1)
	client := extapi.New()

	logger := log.New()

	m := metrics.New()
	d := daemon.New(numWorkers, queueSize, m, logger)
	d.Start(ctx, client)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	api := webapi.New(d, m, logger)

	api.Start()

	defer func(d *daemon.Daemon, a *webapi.API) {
		stop(d, a)
	}(d, api)

	go func() {
		for sig := range c {
			sigHandler(sig, exitCh)
		}
	}()

	if _, ok := <-exitCh; !ok {
		logger.Info("app exit")
		return
	}

	go func() {

	}()
}

func stop(d *daemon.Daemon, api *webapi.API) {
	api.Stop()
	d.Stop()
}

func sigHandler(signal os.Signal, exitCh chan struct{}) {
	switch signal {
	case syscall.SIGTERM:
		close(exitCh)
	case syscall.SIGINT:
		close(exitCh)
	case syscall.SIGKILL:
		close(exitCh)
	default:
		fmt.Printf("signal %s", signal.String())
	}
}
