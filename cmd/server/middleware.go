package main

import (
	"log/slog"
	"net/http"
	"time"
)

// loggingMiddleware логирует HTTP запросы.
// Пропускает логирование статических файлов для снижения шума.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Обёртка response writer для захвата кода статуса.
		wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Пропуск логирования статических файлов.
		if len(r.URL.Path) > 7 && r.URL.Path[:8] == "/static/" {
			return
		}

		slog.Info("request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", wrapped.status),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// responseWriter оборачивает http.ResponseWriter для захвата кода статуса.
// Реализует http.Flusher для поддержки SSE (Server-Sent Events).
type responseWriter struct {
	http.ResponseWriter

	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush реализует интерфейс http.Flusher для SSE.
// Проксирует вызов Flush() к оригинальному ResponseWriter, если он поддерживает Flusher.
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
