package repository

import (
	"submit_service/internal/domain"
	"time"

	"github.com/google/uuid"
)

const (
	timeFormat = "2006-01-02 15:04:05"
)

type TaskRepository struct {
	s *Service
}

func NewTaskRepository(s *Service) *TaskRepository {
	return &TaskRepository{s: s}
}

type TaskDTO struct {
	ID      string            `json:"id"`
	Status  domain.TaskStatus `json:"status"`
	Payload *string           `json:"payload,omitempty"`
	Ts      time.Time         `json:"ts"`
}

func (r *TaskRepository) GetAllNotProcessedTasks() ([]TaskDTO, error) {
	if r.s == nil {
		return nil, nil
	}
	query := "SELECT id FROM tasks WHERE status = $1"
	rows, err := r.s.Client.conn.Query(r.s.Client.ctx, query, domain.StatusFailed)
	if err != nil {
		r.s.logger.WithError(err).Error("Failed to query not processed tasks")
		return nil, err
	}
	defer rows.Close()

	var tasks []TaskDTO
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			r.s.logger.WithError(err).Error("Failed to scan task ID")
			return nil, err
		}
		tasks = append(tasks, TaskDTO{ID: id, Status: domain.StatusFailed})
	}
	return tasks, nil
}

func (r *TaskRepository) UpdateTaskStatus(taskID uuid.UUID, status domain.TaskStatus) error {
	if r.s == nil {
		return nil
	}
	query := "ALTER TABLE tasks UPDATE status = $1 WHERE id = $2 SETTINGS mutations_sync = 1"
	if err := r.s.Client.conn.Exec(r.s.Client.ctx, query, status, taskID); err != nil {
		r.s.logger.WithError(err).Errorf("Failed to update task %s status to %s", taskID, status)
		return err
	}
	return nil
}

func (r *TaskRepository) InsertTask(task *domain.Task) error {
	if r.s == nil {
		return nil
	}
	query := "INSERT INTO tasks (id, status, payload, ts) VALUES ($1, $2, $3, $4)"
	now := time.Now().Format(timeFormat)
	if err := r.s.Client.conn.Exec(r.s.Client.ctx, query, task.ID, task.Status, task.Payload, now); err != nil {
		r.s.logger.WithError(err).Errorf("Failed to insert task %s with status %s", task.ID, task.Status)
		return err
	}
	return nil
}

func (r *TaskRepository) GetTaskByID(taskID uuid.UUID) (*TaskDTO, error) {
	// Здесь будет логика получения задачи по ID из базы данных
	return nil, nil
}
