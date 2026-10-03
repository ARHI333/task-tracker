package service

import (
	"context"
	"errors"
	"task-tracker/internal/repository"
)

// TaskService описывает бизнес-логику работы с задачами
type TaskService interface {
	Create(ctx context.Context, userID int, title, description, status string) (repository.Task, error)
	List(ctx context.Context, userID int, page, limit int, search string) ([]repository.Task, int, error)
	GetByID(ctx context.Context, userID, taskID int) (repository.Task, error)
	Update(ctx context.Context, userID, taskID int, input repository.UpdateInput) (repository.Task, error)
	Delete(ctx context.Context, userID, taskID int) error
}

// taskService – конкретная реализация
type taskService struct {
	repo repository.TaskRepo
}

// NewTaskService создаёт новый сервис
func NewTaskService(repo repository.TaskRepo) TaskService {
	return &taskService{repo: repo}
}

// ------------------------------------------------------------------
// Бизнес-логика
// ------------------------------------------------------------------

func (s *taskService) Create(ctx context.Context, userID int, title, description, status string) (repository.Task, error) {
	if title == "" {
		return repository.Task{}, errors.New("поле title обязательно")
	}
	task := repository.Task{
		Title:       title,
		Description: description,
		Status:      status,
		UserID:      userID,
	}
	return s.repo.Create(ctx, task)
}

func (s *taskService) List(ctx context.Context, userID int, page, limit int, search string) ([]repository.Task, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	return s.repo.List(ctx, userID, page, limit, search)
}

func (s *taskService) GetByID(ctx context.Context, userID, taskID int) (repository.Task, error) {
	task, err := s.repo.GetByID(ctx, userID, taskID)
	if err != nil {
		return repository.Task{}, err
	}
	// Дополнительная проверка: если задача не принадлежит пользователю, вернём ошибку
	if task.UserID != userID {
		return repository.Task{}, errors.New("задача не найдена")
	}
	return task, nil
}

func (s *taskService) Update(ctx context.Context, userID, taskID int, input repository.UpdateInput) (repository.Task, error) {
	// Проверяем, что задача существует и принадлежит пользователю
	_, err := s.GetByID(ctx, userID, taskID)
	if err != nil {
		return repository.Task{}, err
	}
	return s.repo.Update(ctx, taskID, userID, input)
}

func (s *taskService) Delete(ctx context.Context, userID, taskID int) error {
	// Аналогично проверяем существование
	_, err := s.GetByID(ctx, userID, taskID)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, userID, taskID)
}
