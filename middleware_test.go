package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoggingMiddleware(t *testing.T) {
	// Фейковый следующий обработчик, который записывает, что был вызван
	called := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot) // 418 I'm a teapot, просто для разнообразия
	})

	// Оборачиваем в middleware
	wrapped := loggingMiddleware(nextHandler)

	req := httptest.NewRequest("GET", "/any", nil)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req) // для http.Handler используем ServeHTTP

	assert.True(t, called, "Следующий обработчик должен быть вызван")
	assert.Equal(t, http.StatusTeapot, rr.Code, "Код ответа должен быть 418")
}
