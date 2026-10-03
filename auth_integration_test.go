//go:build integration
// +build integration

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterAndLoginIntegration(t *testing.T) {
	if !*integration {
		t.Skip("пропускаем интеграционный тест; запустите с -integration")
	}

	pool := setupTestDB(t)
	defer pool.Close()

	originalPool := dbPool
	dbPool = pool
	defer func() { dbPool = originalPool }()

	// 1. Регистрация
	body := []byte(`{"username":"testuser","password":"testpass"}`)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	registerHandler(rr, req)
	assert.Equal(t, http.StatusCreated, rr.Code)

	// 2. Вход
	req = httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	loginHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]string
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp["token"])
}
