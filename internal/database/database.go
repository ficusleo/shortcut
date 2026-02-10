package database

import "sync"

type Config struct {
}

type Service struct {
	mux     *sync.Mutex
	storage map[string]struct{}
}

func New(conf *Config) *Service {
	return &Service{
		storage: make(map[string]struct{}),
		mux:     &sync.Mutex{},
	}
}

func (s *Service) AddNotProcessedTask(taskID string) {
	s.mux.Lock()
	defer s.mux.Unlock()

	s.storage[taskID] = struct{}{}
}

func (s *Service) GetAllNotProcessedTasks() []string {
	s.mux.Lock()
	defer s.mux.Unlock()

	tasks := make([]string, 0, len(s.storage))
	for taskID := range s.storage {
		tasks = append(tasks, taskID)
	}
	return tasks
}
