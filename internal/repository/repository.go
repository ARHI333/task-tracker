package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Task — модель задачи
type Task struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UserID      int    `json:"-"`
}

// TaskRepo — интерфейс работы с задачами. Позже мы легко сможем заменить реальную БД на мок для тестов.
type TaskRepo interface {
	Create(ctx context.Context, task Task) (Task, error)
	List(ctx context.Context, userID int, page, limit int, search string) ([]Task, int, error)
	GetByID(ctx context.Context, userID, taskID int) (Task, error)
	Update(ctx context.Context, taskID int, userID int, input UpdateInput) (Task, error)
	Delete(ctx context.Context, userID, taskID int) error
}

// UpdateInput — что можно обновить у задачи
type UpdateInput struct {
	Title       *string
	Description *string
	Status      *string
}

// PostgresTaskRepo — конкретная реализация для PostgreSQL
type PostgresTaskRepo struct {
	pool *pgxpool.Pool
}

// NewPostgresTaskRepo — конструктор
func NewPostgresTaskRepo(pool *pgxpool.Pool) *PostgresTaskRepo {
	return &PostgresTaskRepo{pool: pool}
}
func (r *PostgresTaskRepo) Create(ctx context.Context, task Task) (Task, error) {
	err := r.pool.QueryRow(
		ctx,
		"INSERT INTO tasks (title, description, status, user_id) VALUES ($1, $2, $3, $4) RETURNING id, created_at",
		task.Title, task.Description, task.Status, task.UserID,
	).Scan(&task.ID, &task.CreatedAt)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

// List возвращает список задач пользователя с пагинацией и поиском
func (r *PostgresTaskRepo) List(ctx context.Context, userID int, page, limit int, search string) ([]Task, int, error) {
	var total int
	countQuery := `SELECT COUNT(*) FROM tasks WHERE user_id = $1`
	countArgs := []interface{}{userID}

	query := `SELECT id, title, description, status, created_at, user_id FROM tasks WHERE user_id = $1`
	args := []interface{}{userID}

	if search != "" {
		searchClause := ` AND (title ILIKE '%' || $2 || '%' OR description ILIKE '%' || $2 || '%')`
		query += searchClause
		countQuery += searchClause
		args = append(args, search)
		countArgs = append(countArgs, search)
	}

	err := r.pool.QueryRow(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	query += ` ORDER BY id ASC LIMIT $` + fmt.Sprintf("%d", len(args)+1) + ` OFFSET $` + fmt.Sprintf("%d", len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.CreatedAt, &t.UserID)
		if err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, t)
	}

	if tasks == nil {
		tasks = []Task{}
	}

	return tasks, total, nil
}

// GetByID возвращает задачу по ID и userID
func (r *PostgresTaskRepo) GetByID(ctx context.Context, userID, taskID int) (Task, error) {
	var t Task
	err := r.pool.QueryRow(
		ctx,
		"SELECT id, title, description, status, created_at, user_id FROM tasks WHERE id = $1 AND user_id = $2",
		taskID, userID,
	).Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.CreatedAt, &t.UserID)
	if err != nil {
		return Task{}, err
	}
	return t, nil
}

// Update частично обновляет задачу
func (r *PostgresTaskRepo) Update(ctx context.Context, taskID int, userID int, input UpdateInput) (Task, error) {
	setClauses := []string{}
	args := []interface{}{}
	argPos := 1

	if input.Title != nil {
		setClauses = append(setClauses, fmt.Sprintf("title = $%d", argPos))
		args = append(args, *input.Title)
		argPos++
	}
	if input.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argPos))
		args = append(args, *input.Description)
		argPos++
	}
	if input.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argPos))
		args = append(args, *input.Status)
		argPos++
	}

	if len(setClauses) == 0 {
		// Нет полей для обновления — просто возвращаем текущую задачу
		return r.GetByID(ctx, userID, taskID)
	}

	query := "UPDATE tasks SET " + joinStrings(setClauses, ", ") +
		fmt.Sprintf(" WHERE id = $%d AND user_id = $%d", argPos, argPos+1)
	args = append(args, taskID, userID)

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return Task{}, err
	}

	return r.GetByID(ctx, userID, taskID)
}

// Delete удаляет задачу
func (r *PostgresTaskRepo) Delete(ctx context.Context, userID, taskID int) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM tasks WHERE id = $1 AND user_id = $2", taskID, userID)
	return err
}

// Вспомогательная функция для склеивания строк
func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
