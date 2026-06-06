package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// ---------- JWT и контекст ----------
var jwtSecret = []byte("my-secret-key")

type contextKey string

const userIDKey contextKey = "userID"

// ---------- Структуры ----------
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type Task struct {
	ID          int       `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// PatchTaskInput — для частичного обновления задачи (PATCH)
type PatchTaskInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Status      *string `json:"status"`
}

// TaskListResponse — ответ для GET /tasks с пагинацией
type TaskListResponse struct {
	Tasks []Task `json:"tasks"`
	Total int    `json:"total"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

// ---------- Глобальный пул БД ----------
var dbPool *pgxpool.Pool

// ---------- Вспомогательные функции ----------
func getUserID(r *http.Request) int {
	userID, ok := r.Context().Value(userIDKey).(int)
	if !ok {
		return 0 // такого не должно быть при правильной цепочке middleware
	}
	return userID
}

// ---------- Middleware ----------
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("→ %s %s", r.Method, r.URL.Path)
		next(w, r)
		log.Printf("← %s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	}
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Токен не предоставлен", http.StatusUnauthorized)
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			http.Error(w, "Неверный формат токена", http.StatusUnauthorized)
			return
		}

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("неверный метод подписи")
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Неверный или просроченный токен", http.StatusUnauthorized)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			http.Error(w, "Неверные claims", http.StatusUnauthorized)
			return
		}

		userIDFloat, ok := claims["user_id"].(float64)
		if !ok {
			http.Error(w, "Нет user_id в токене", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, int(userIDFloat))
		next(w, r.WithContext(ctx))
	}
}

// ---------- Обработчики аутентификации ----------
func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST", http.StatusMethodNotAllowed)
		return
	}

	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	if creds.Username == "" || creds.Password == "" {
		http.Error(w, "Логин и пароль обязательны", http.StatusBadRequest)
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(creds.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "Ошибка сервера", http.StatusInternalServerError)
		return
	}

	_, err = dbPool.Exec(
		context.Background(),
		"INSERT INTO users (username, password_hash) VALUES ($1, $2)",
		creds.Username, string(hashedPassword),
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			http.Error(w, "Пользователь уже существует", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("Ошибка сохранения: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"message": "Пользователь создан"})
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Только POST", http.StatusMethodNotAllowed)
		return
	}

	var creds Credentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	if creds.Username == "" || creds.Password == "" {
		http.Error(w, "Логин и пароль обязательны", http.StatusBadRequest)
		return
	}

	var userID int
	var passwordHash string
	err := dbPool.QueryRow(
		context.Background(),
		"SELECT id, password_hash FROM users WHERE username = $1",
		creds.Username,
	).Scan(&userID, &passwordHash)

	if err != nil {
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(creds.Password)); err != nil {
		http.Error(w, "Неверный логин или пароль", http.StatusUnauthorized)
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  userID,
		"username": creds.Username,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		http.Error(w, "Ошибка создания токена", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": tokenString})
}

// ---------- Обработчики задач ----------
func tasksHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		createTask(w, r)
	case http.MethodGet:
		listTasks(w, r)
	default:
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)
	}
}

func createTask(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения", http.StatusBadRequest)
		return
	}

	var task Task
	if err := json.Unmarshal(body, &task); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	if task.Title == "" {
		http.Error(w, "Поле 'title' обязательно", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	var newID int
	var createdAt time.Time
	err = dbPool.QueryRow(
		context.Background(),
		"INSERT INTO tasks (title, description, status, user_id) VALUES ($1, $2, $3, $4) RETURNING id, created_at",
		task.Title, task.Description, task.Status, userID,
	).Scan(&newID, &createdAt)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка сохранения: %v", err), http.StatusInternalServerError)
		return
	}

	task.ID = newID
	task.CreatedAt = createdAt

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

func listTasks(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)

	// ---------- Чтение query-параметров ----------
	// page и limit — с значениями по умолчанию
	pageStr := r.URL.Query().Get("page")
	limitStr := r.URL.Query().Get("limit")
	search := r.URL.Query().Get("search") // может быть пустым

	page := 1
	limit := 10 // значение по умолчанию

	if pageStr != "" {
		var err error
		page, err = strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			http.Error(w, "Параметр 'page' должен быть положительным целым числом", http.StatusBadRequest)
			return
		}
	}

	if limitStr != "" {
		var err error
		limit, err = strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > 100 {
			http.Error(w, "Параметр 'limit' должен быть целым числом от 1 до 100", http.StatusBadRequest)
			return
		}
	}

	offset := (page - 1) * limit

	// ---------- Построение запроса ----------
	// Базовый запрос с фильтром по пользователю
	query := `SELECT id, title, description, status, created_at FROM tasks WHERE user_id = $1`
	countQuery := `SELECT COUNT(*) FROM tasks WHERE user_id = $1`

	// Аргументы для запросов
	args := []interface{}{userID}
	countArgs := []interface{}{userID}

	// Добавляем поиск, если задан
	if search != "" {
		// ILIKE делает поиск регистронезависимым
		searchClause := ` AND (title ILIKE '%' || $2 || '%' OR description ILIKE '%' || $2 || '%')`
		query += searchClause
		countQuery += searchClause
		args = append(args, search)
		countArgs = append(countArgs, search)
	}

	// Сначала получаем общее количество записей (для пагинации)
	var total int
	err := dbPool.QueryRow(context.Background(), countQuery, countArgs...).Scan(&total)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка подсчёта задач: %v", err), http.StatusInternalServerError)
		return
	}

	// Добавляем сортировку и пагинацию
	query += ` ORDER BY id ASC LIMIT $` + strconv.Itoa(len(args)+1) + ` OFFSET $` + strconv.Itoa(len(args)+2)
	args = append(args, limit, offset)

	// Выполняем основной запрос
	rows, err := dbPool.Query(context.Background(), query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка запроса: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var t Task
		err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.CreatedAt)
		if err != nil {
			http.Error(w, fmt.Sprintf("Ошибка чтения строки: %v", err), http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, t)
	}

	// Гарантируем, что tasks не будет nil (для красивого JSON)
	if tasks == nil {
		tasks = []Task{}
	}

	// Формируем ответ
	resp := TaskListResponse{
		Tasks: tasks,
		Total: total,
		Page:  page,
		Limit: limit,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func taskByIDHandler(w http.ResponseWriter, r *http.Request) {
	idStr := strings.TrimPrefix(r.URL.Path, "/tasks/")
	if idStr == "" {
		http.Error(w, "ID не указан", http.StatusBadRequest)
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "ID должен быть числом", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		getTask(w, r, id)
	case http.MethodPut:
		updateTask(w, r, id)
	case http.MethodPatch: // <-- новый метод
		updateTaskPartial(w, r, id)
	case http.MethodDelete:
		deleteTask(w, r, id)
	default:
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)

	}
}

func getTask(w http.ResponseWriter, r *http.Request, id int) {
	var t Task
	err := dbPool.QueryRow(
		context.Background(),
		"SELECT id, title, description, status, created_at FROM tasks WHERE id = $1 AND user_id = $2",
		id, getUserID(r),
	).Scan(&t.ID, &t.Title, &t.Description, &t.Status, &t.CreatedAt)

	if err != nil {
		http.Error(w, "Задача не найдена", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(t)
}

func updateTask(w http.ResponseWriter, r *http.Request, id int) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения", http.StatusBadRequest)
		return
	}

	var input Task
	if err := json.Unmarshal(body, &input); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	_, err = dbPool.Exec(
		context.Background(),
		"UPDATE tasks SET title = $1, description = $2, status = $3 WHERE id = $4 AND user_id = $5",
		input.Title, input.Description, input.Status, id, getUserID(r),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка обновления: %v", err), http.StatusInternalServerError)
		return
	}

	getTask(w, r, id) // возвращаем обновлённую задачу
}

func deleteTask(w http.ResponseWriter, r *http.Request, id int) {
	_, err := dbPool.Exec(context.Background(),
		"DELETE FROM tasks WHERE id = $1 AND user_id = $2",
		id, getUserID(r),
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка удаления: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ---------- Инициализация таблиц ----------
// updateTaskPartial обрабатывает PATCH /tasks/{id}
func updateTaskPartial(w http.ResponseWriter, r *http.Request, id int) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения", http.StatusBadRequest)
		return
	}

	var input PatchTaskInput
	if err := json.Unmarshal(body, &input); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	// Проверяем, что хотя бы одно поле передано
	if input.Title == nil && input.Description == nil && input.Status == nil {
		http.Error(w, "Нет полей для обновления", http.StatusBadRequest)
		return
	}

	// Динамически строим SQL
	setClauses := []string{}
	args := []interface{}{} // аргументы для SQL
	argPos := 1             // счётчик позиций $1, $2...

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

	// Собираем запрос: UPDATE tasks SET ... WHERE id = $N AND user_id = $M
	query := "UPDATE tasks SET " + strings.Join(setClauses, ", ") +
		fmt.Sprintf(" WHERE id = $%d AND user_id = $%d", argPos, argPos+1)
	args = append(args, id, getUserID(r))

	_, err = dbPool.Exec(context.Background(), query, args...)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка обновления: %v", err), http.StatusInternalServerError)
		return
	}

	// Возвращаем обновлённую задачу
	getTask(w, r, id)
}
func createTable() {
	_, err := dbPool.Exec(context.Background(), "DROP TABLE IF EXISTS tasks CASCADE")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка удаления tasks: %v\n", err)
		os.Exit(1)
	}

	sql := `
        CREATE TABLE tasks (
            id SERIAL PRIMARY KEY,
            title TEXT NOT NULL,
            description TEXT NOT NULL DEFAULT '',
            status TEXT NOT NULL DEFAULT 'pending',
            created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
            user_id INTEGER REFERENCES users(id) ON DELETE CASCADE
        )
    `
	_, err = dbPool.Exec(context.Background(), sql)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка создания tasks: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Таблица tasks готова")
}

func createUserTable() {
	_, err := dbPool.Exec(context.Background(), "DROP TABLE IF EXISTS users CASCADE")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка удаления users: %v\n", err)
		os.Exit(1)
	}

	sql := `
        CREATE TABLE users (
            id SERIAL PRIMARY KEY,
            username TEXT NOT NULL UNIQUE,
            password_hash TEXT NOT NULL,
            created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
        )
    `
	_, err = dbPool.Exec(context.Background(), sql)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Ошибка создания users: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Таблица users готова")
}

// ---------- Главная функция ----------
func main() {
	connStr := "postgres://postgres:secret@localhost:5432/postgres"
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось подключиться к базе: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	dbPool = pool

	createTable()
	createUserTable()

	http.HandleFunc("/tasks", loggingMiddleware(authMiddleware(tasksHandler)))
	http.HandleFunc("/tasks/", loggingMiddleware(authMiddleware(taskByIDHandler)))
	http.HandleFunc("/register", loggingMiddleware(registerHandler))
	http.HandleFunc("/login", loggingMiddleware(loginHandler))

	fmt.Println("Task Tracker запущен на http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
