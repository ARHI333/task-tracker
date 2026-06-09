markdown
# Task Tracker API

REST API для управления задачами с аутентификацией и авторизацией.
Реализован на Go с использованием PostgreSQL.

## Технологии

- **Go** 1.21+
- **PostgreSQL** 16 (в Docker)
- **pgx** — драйвер PostgreSQL
- **JWT** — аутентификация
- **bcrypt** — хеширование паролей
- **Docker** — контейнеризация базы данных

## Быстрый старт

### 1. Запустить PostgreSQL в Docker

```bash
docker run --name my-postgres -e POSTGRES_PASSWORD=secret -p 5432:5432 -d postgres:16-alpine
2. Клонировать и запустить проект
bash
git clone https://github.com/yourusername/task-tracker.git
cd task-tracker
go mod tidy
go run main.go
Сервер запустится на http://localhost:8080.

Аутентификация
Все эндпоинты, кроме /register и /login, требуют заголовок:
Authorization: Bearer <токен>

Регистрация
bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{"username":"ivan","password":"123456"}'
Вход (получение токена)
bash
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username":"ivan","password":"123456"}'
Ответ:

json
{"token":"eyJhbGciOiJIUzI1NiIs..."}
API Эндпоинты
Задачи
Создать задачу
http
POST /tasks
Тело:

json
{
  "title": "Купить продукты",
  "description": "Молоко, хлеб",
  "status": "pending"
}
Ответ: 201 Created с объектом задачи.

Список задач с пагинацией и поиском
http
GET /tasks?page=1&limit=10&search=продукт
Ответ:

json
{
  "tasks": [ ... ],
  "total": 42,
  "page": 1,
  "limit": 10
}
Получить задачу по ID
http
GET /tasks/1
Полное обновление (PUT)
http
PUT /tasks/1
Content-Type: application/json

{
  "title": "Новый заголовок",
  "description": "Новое описание",
  "status": "done"
}
Частичное обновление (PATCH)
http
PATCH /tasks/1
Content-Type: application/json

{
  "status": "done"
}
Обновляются только переданные поля.

Удалить задачу
http
DELETE /tasks/1
Ответ: 204 No Content.

Обработка ошибок
API возвращает стандартные HTTP-коды:

400 — неверный запрос (валидация)

401 — неверный токен или отсутствует

404 — задача не найдена

409 — пользователь уже существует

500 — внутренняя ошибка сервера

Структура проекта
text
task-tracker/
├── main.go          # Весь код сервера
├── go.mod           # Зависимости
├── go.sum           # Контрольные суммы
└── README.md        # Документация
Автор
ARHI333 — начинающий Go-разработчик.
Проект создан в рамках интенсивного обучения программированию с нуля.

text

---

## Что делать дальше

1. **Замени** `https://github.com/yourusername/task-tracker.git` на реальную ссылку, если захочешь залить проект на GitHub.
2. **Укажи своё имя** в разделе «Автор».
3. **Залей проект на GitHub** (если ещё нет) — это обязательный шаг для портфолио.

Когда файл будет готов, напиши **«README готов»**. После этого я подведу итоги нашего обучения и предложу план следующих шагов для выхода на рынок. Ты проделал огромный путь!