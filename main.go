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
	"task-tracker/internal/repository"
	repo "task-tracker/internal/repository"
	"task-tracker/internal/service"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// ---------- JWT и контекст ----------
var jwtSecret = []byte("my-secret-key")
var taskRepo repository.TaskRepo // глобальная переменная
var taskService service.TaskService

type contextKey string

const userIDKey contextKey = "userID"

// ---------- Структуры ----------
type Credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// TaskListResponse — ответ для GET /tasks с пагинацией
type TaskListResponse struct {
	Tasks []repo.Task `json:"tasks"`
	Total int         `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
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

	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	task, err := taskService.Create(context.Background(), userID, input.Title, input.Description, input.Status)
	if err != nil {
		if err.Error() == "поле title обязательно" {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, fmt.Sprintf("Ошибка сохранения: %v", err), http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(task)
}

func listTasks(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	search := r.URL.Query().Get("search")

	tasks, total, err := taskService.List(context.Background(), userID, page, limit, search)
	if err != nil {
		http.Error(w, fmt.Sprintf("Ошибка запроса: %v", err), http.StatusInternalServerError)
		return
	}

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
	case http.MethodDelete:
		deleteTask(w, r, id)
	default:
		http.Error(w, "Метод не разрешён", http.StatusMethodNotAllowed)

	}
}

func getTask(w http.ResponseWriter, r *http.Request, id int) {
	userID := getUserID(r)
	task, err := taskService.GetByID(context.Background(), userID, id)
	if err != nil {
		http.Error(w, "Задача не найдена", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func updateTask(w http.ResponseWriter, r *http.Request, id int) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Ошибка чтения", http.StatusBadRequest)
		return
	}

	var input struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(body, &input); err != nil {
		http.Error(w, "Неверный JSON", http.StatusBadRequest)
		return
	}

	userID := getUserID(r)
	updateInput := repository.UpdateInput{
		Title:       &input.Title,
		Description: &input.Description,
		Status:      &input.Status,
	}
	task, err := taskService.Update(context.Background(), userID, id, updateInput)
	if err != nil {
		http.Error(w, "Задача не найдена", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(task)
}

func deleteTask(w http.ResponseWriter, r *http.Request, id int) {
	userID := getUserID(r)
	if err := taskService.Delete(context.Background(), userID, id); err != nil {
		http.Error(w, "Задача не найдена", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// ---------- Главная функция ----------
func main() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://postgres:secret@localhost:5432/postgres"
	}
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Не удалось подключиться к базе: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()
	dbPool = pool
	// Ждём, пока база данных будет готова принимать запросы
	for {
		err := dbPool.Ping(context.Background())
		if err == nil {
			break
		}
		log.Println("Ожидание готовности базы данных...")
		time.Sleep(2 * time.Second)
	}

	createUserTable() // сначала users, потому что tasks ссылается на users
	createTable()     // потом tasks
	taskRepo = repository.NewPostgresTaskRepo(pool)
	taskService = service.NewTaskService(taskRepo)
	http.HandleFunc("/tasks", loggingMiddleware(authMiddleware(tasksHandler)))
	http.HandleFunc("/tasks/", loggingMiddleware(authMiddleware(taskByIDHandler)))
	http.HandleFunc("/register", loggingMiddleware(registerHandler))
	http.HandleFunc("/login", loggingMiddleware(loginHandler))
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Task Tracker запущен на http://localhost:8080")
	http.ListenAndServe(":8080", nil)

}
