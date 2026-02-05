package extapi

import (
	"context"
	"math/rand"
	"time"

	log "github.com/sirupsen/logrus"

	_ "net/http/pprof"
)

type CustomError struct {
	Msg string
}

func (e *CustomError) Error() string {
	return e.Msg
}

type ExternalAPIImplementation struct {
	TaskID string
}

type Client struct {
	API *ExternalAPIImplementation
}

func New() *Client {
	return &Client{
		API: &ExternalAPIImplementation{},
	}
}

func (c *Client) SetTaskID(taskID string) {
	c.API.TaskID = taskID
}

func (c *Client) GetSomething(ctx context.Context, workerID int) error {
	startedAt := time.Now()
	sleepDuration := time.Duration(1000+rand.Intn(10000)) * time.Millisecond
	if rand.Intn(10) == 0 {
		return &CustomError{Msg: "External API simulated failure"}
	}
	select {
	case <-ctx.Done():
		log.WithFields(log.Fields{"workerId": workerID, "taskId": c.API.TaskID, "duration": time.Since(startedAt)}).Info("External API call cancelled")
		return ctx.Err()
	case <-time.After(sleepDuration):
		log.WithFields(log.Fields{"workerId": workerID, "taskId": c.API.TaskID, "duration": time.Since(startedAt)}).Info("External API call completed")
		return nil
	}
}
