package domain

import "github.com/google/uuid"

type Task struct {
	ID      uuid.UUID
	Status  TaskStatus
	Payload *string
}

type TaskStatus string

const (
	StatusFailed     TaskStatus = "failed"
	StatusProcessing TaskStatus = "processing"
	StatusPending    TaskStatus = "pending"
	StatusProcessed  TaskStatus = "done"
)
