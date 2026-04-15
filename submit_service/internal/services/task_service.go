package services

import (
	"github.com/google/uuid"

	"submit_service/internal/domain"
	"submit_service/internal/repository"
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
		taskId, err := uuid.Parse(t.ID)
		if err != nil {
			continue
		}
		tasks = append(tasks, domain.Task{
			ID:      taskId,
			Status:  t.Status,
			Payload: t.Payload,
		})
	}
	return tasks, nil
}

func (s *TaskService) UpdateTaskStatus(taskId uuid.UUID, status domain.TaskStatus) error {
	return s.taskRepo.UpdateTaskStatus(taskId, status)
}

func (s *TaskService) InsertTask(task *domain.Task) error {
	return s.taskRepo.InsertTask(task)
}

func (s *TaskService) GetTaskByID(taskId uuid.UUID) (*domain.Task, error) {
	taskDTO, err := s.taskRepo.GetTaskByID(taskId)
	if err != nil {
		return nil, err
	}
	if taskDTO == nil {
		return nil, nil
	}
	return &domain.Task{
		ID:      uuid.MustParse(taskDTO.ID),
		Status:  taskDTO.Status,
		Payload: taskDTO.Payload,
	}, nil
}

func (s *TaskService) GetTaskStatusByTaskID(taskId uuid.UUID) (domain.TaskStatus, error) {
	taskDTO, err := s.taskRepo.GetTaskByID(taskId)
	if err != nil {
		return "", err
	}
	if taskDTO == nil {
		return "", nil
	}
	return taskDTO.Status, nil
}