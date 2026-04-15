package services

import (
	"context"

	"github.com/google/uuid"

	"process_service/internal/domain"
	"process_service/internal/repository"
)

type TaskService struct {
	taskRepo *repository.TaskRepository
}

func NewTaskService(taskRepo *repository.TaskRepository) *TaskService {
	return &TaskService{taskRepo: taskRepo}
}

func (s *TaskService) GetAllNotProcessedTasks() ([]domain.Task, error) {
	tasks := make([]domain.Task, 0)
	tasksDB, err := s.taskRepo.GetAllNotProcessedTasks()
	if err != nil {
		return nil, err
	}
	for _, t := range tasksDB {
		taskID, err := uuid.Parse(t.ID)
		if err != nil {
			continue
		}
		tasks = append(tasks, domain.Task{
			ID:      taskID,
			Status:  t.Status,
			Payload: t.Payload,
			FailedPayload: t.FailedPayload,
		})
	}
	return tasks, nil
}

func (s *TaskService) UpdateTaskStatus(taskID uuid.UUID, status domain.TaskStatus) error {
	return s.taskRepo.UpdateTaskStatus(taskID, status)
}

func (s *TaskService) InsertTask(task *domain.Task) error {
	return s.taskRepo.InsertTask(task)
}

func (s *TaskService) GetTaskByID(taskID uuid.UUID) (*domain.Task, error) {
	taskDTO := s.taskRepo.GetTaskByID(taskID)
	if taskDTO == nil {
		return nil, nil
	}
	return &domain.Task{
		ID:      uuid.MustParse(taskDTO.ID),
		Status:  taskDTO.Status,
		Payload: taskDTO.Payload,
		FailedPayload: taskDTO.FailedPayload,
	}, nil
}

func (s *TaskService) MarkTaskInvalid(ctx context.Context, taskID uuid.UUID, failedPayload *string, reason string) error {
	return s.taskRepo.MarkTaskInvalid(ctx, taskID, failedPayload, reason)
}
