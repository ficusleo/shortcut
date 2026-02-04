package extapi

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

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

func (e *ExternalAPIImplementation) SetTaskID(taskID string) {
	e.TaskID = taskID
}

func (e *ExternalAPIImplementation) GetSomething(ctx context.Context, workerID int) error {
	sleepDuration := time.Duration(1000+rand.Intn(10000)) * time.Millisecond
	if rand.Intn(10) == 0 {
		return &CustomError{Msg: fmt.Sprintf("[Worker %d][%s]: Simulated external API failure", workerID, e.TaskID)}
	}
	select {
	case <-ctx.Done():
		log.Printf("[Worker %d][%s]: External API call cancelled", workerID, e.TaskID)
		return ctx.Err()
	case <-time.After(sleepDuration):
		log.Printf("[Worker %d][%s]: External API call completed in %v", workerID, e.TaskID, sleepDuration)
		return nil
	}
}
