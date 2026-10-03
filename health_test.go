package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthHandler(t *testing.T) {
	// Создаём фейковый HTTP-запрос к /health
	req, err := http.NewRequest("GET", "/health", nil)
	assert.NoError(t, err)

	// Создаём ResponseRecorder, чтобы перехватить ответ
	rr := httptest.NewRecorder()

	// Вызываем наш обработчик
	healthHandler(rr, req)

	// Проверяем код ответа
	assert.Equal(t, http.StatusOK, rr.Code, "Статус должен быть 200")

	// Проверяем тело ответа
	assert.Equal(t, "OK", rr.Body.String(), "Тело должно быть 'OK'")
}
