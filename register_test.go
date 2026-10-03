package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterHandler_EmptyFields(t *testing.T) {
	// Запрос с пустым логином
	body := []byte(`{"username":"","password":"secret"}`)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	registerHandler(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "Логин и пароль обязательны")
}

func TestRegisterHandler_WrongMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/register", nil)
	rr := httptest.NewRecorder()

	registerHandler(rr, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	assert.Contains(t, rr.Body.String(), "Только POST")
}
