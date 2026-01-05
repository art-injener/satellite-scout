package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingMiddleware(t *testing.T) {
	// Создаём тестовый handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("test response")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	})

	// Оборачиваем в middleware
	handler := loggingMiddleware(testHandler)

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{
			name:       "regular request",
			path:       "/api/health",
			wantStatus: http.StatusOK,
		},
		{
			name:       "static file request",
			path:       "/static/css/main.css",
			wantStatus: http.StatusOK,
		},
		{
			name:       "root request",
			path:       "/",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			resp := w.Result()
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Expected status %d, got %d", tt.wantStatus, resp.StatusCode)
			}
		})
	}
}

func TestResponseWriter_WriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	rw.WriteHeader(http.StatusNotFound)

	if rw.status != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, rw.status)
	}

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected response code %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestResponseWriter_DefaultStatus(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	// Пишем без явного вызова WriteHeader
	if _, err := rw.Write([]byte("test")); err != nil {
		t.Errorf("Failed to write: %v", err)
	}

	// Статус должен остаться 200
	if rw.status != http.StatusOK {
		t.Errorf("Expected default status %d, got %d", http.StatusOK, rw.status)
	}
}

func TestResponseWriter_MultipleWriteHeader(t *testing.T) {
	w := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
	}

	// Первый вызов
	rw.WriteHeader(http.StatusCreated)
	if rw.status != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, rw.status)
	}

	// Второй вызов (должен быть проигнорирован http.ResponseWriter)
	rw.WriteHeader(http.StatusBadRequest)

	// Наш wrapper обновит статус, но реальный ResponseWriter не изменится
	if rw.status != http.StatusBadRequest {
		t.Errorf("Expected wrapper status %d, got %d", http.StatusBadRequest, rw.status)
	}
}

func TestSlogKeyError(t *testing.T) {
	if slogKeyError != "error" {
		t.Errorf("Expected slogKeyError to be 'error', got '%s'", slogKeyError)
	}
}
