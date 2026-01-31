package tracker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestCelestrakClient_FetchByNoradID тестирует загрузку TLE по NORAD ID
func TestCelestrakClient_FetchByNoradID(t *testing.T) {
	// Mock сервер с TLE данными ISS
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Проверяем параметры запроса
		if !strings.Contains(r.URL.RawQuery, "CATNR=25544") {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(r.URL.RawQuery, "FORMAT=TLE") {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Возвращаем TLE ISS
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ISS (ZARYA)\n" + issLine1 + "\n" + issLine2))
	}))
	defer server.Close()

	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(0), // Отключаем rate limit для тестов
	)

	ctx := context.Background()
	tle, err := client.FetchByNoradID(ctx, 25544)
	if err != nil {
		t.Fatalf("FetchByNoradID() error = %v", err)
	}

	if tle.NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544", tle.NoradID)
	}
	if tle.Name != "ISS (ZARYA)" {
		t.Errorf("Name = %q, want %q", tle.Name, "ISS (ZARYA)")
	}
}

// TestCelestrakClient_FetchByNoradID_NotFound тестирует обработку 404
func TestCelestrakClient_FetchByNoradID_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("No GP data found"))
	}))
	defer server.Close()

	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(0),
		WithMaxRetries(0),
	)

	ctx := context.Background()
	_, err := client.FetchByNoradID(ctx, 99999)
	if err == nil {
		t.Error("FetchByNoradID() expected error for not found, got nil")
	}
}

// TestCelestrakClient_FetchGroup тестирует загрузку группы спутников
func TestCelestrakClient_FetchGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "GROUP=stations") {
			http.NotFound(w, r)
			return
		}

		// Возвращаем несколько TLE
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(issTLE + "\n" + meteorTLE))
	}))
	defer server.Close()

	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(0),
	)

	ctx := context.Background()
	tles, err := client.FetchGroup(ctx, GroupStations)
	if err != nil {
		t.Fatalf("FetchGroup() error = %v", err)
	}

	if len(tles) != 2 {
		t.Errorf("FetchGroup() returned %d TLEs, want 2", len(tles))
	}
}

// TestCelestrakClient_RateLimit тестирует соблюдение rate limit
func TestCelestrakClient_RateLimit(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ISS (ZARYA)\n" + issLine1 + "\n" + issLine2))
	}))
	defer server.Close()

	rateLimit := 100 * time.Millisecond
	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(rateLimit),
	)

	ctx := context.Background()
	start := time.Now()

	// Делаем 3 запроса
	for i := 0; i < 3; i++ {
		_, _ = client.FetchByNoradID(ctx, 25544)
	}

	elapsed := time.Since(start)

	// Должно пройти минимум 2 * rateLimit (между 3 запросами 2 паузы)
	expectedMin := 2 * rateLimit
	if elapsed < expectedMin {
		t.Errorf("Rate limit not respected: elapsed %v, expected at least %v", elapsed, expectedMin)
	}

	if requestCount != 3 {
		t.Errorf("Request count = %d, want 3", requestCount)
	}
}

// TestCelestrakClient_Retry тестирует повторные попытки при ошибках
func TestCelestrakClient_Retry(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ISS (ZARYA)\n" + issLine1 + "\n" + issLine2))
	}))
	defer server.Close()

	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(0),
		WithMaxRetries(3),
	)

	ctx := context.Background()
	tle, err := client.FetchByNoradID(ctx, 25544)
	if err != nil {
		t.Fatalf("FetchByNoradID() error = %v", err)
	}

	if tle.NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544", tle.NoradID)
	}

	if attemptCount != 3 {
		t.Errorf("Attempt count = %d, want 3", attemptCount)
	}
}

// TestCelestrakClient_ContextCancellation тестирует отмену контекста
func TestCelestrakClient_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // Долгий ответ
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(0),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.FetchByNoradID(ctx, 25544)
	if err == nil {
		t.Error("FetchByNoradID() expected error for context cancellation, got nil")
	}
}

// TestGetGroupURL тестирует формирование URL для группы
func TestGetGroupURL(t *testing.T) {
	url := GetGroupURL(GroupStations)
	expected := "https://celestrak.org/NORAD/elements/gp.php?GROUP=stations&FORMAT=TLE"
	if url != expected {
		t.Errorf("GetGroupURL() = %q, want %q", url, expected)
	}
}

// TestGetNoradURL тестирует формирование URL для NORAD ID
func TestGetNoradURL(t *testing.T) {
	url := GetNoradURL(25544)
	expected := "https://celestrak.org/NORAD/elements/gp.php?CATNR=25544&FORMAT=TLE"
	if url != expected {
		t.Errorf("GetNoradURL() = %q, want %q", url, expected)
	}
}

// TestAvailableGroups проверяет список доступных групп
func TestAvailableGroups(t *testing.T) {
	groups := AvailableGroups()
	if len(groups) == 0 {
		t.Error("AvailableGroups() returned empty list")
	}

	// Проверяем наличие основных групп
	groupSet := make(map[SatelliteGroup]bool)
	for _, g := range groups {
		groupSet[g] = true
	}

	requiredGroups := []SatelliteGroup{
		GroupStations, GroupAmateur, GroupCubesat, GroupStarlink,
	}

	for _, g := range requiredGroups {
		if !groupSet[g] {
			t.Errorf("AvailableGroups() missing required group %q", g)
		}
	}
}

// TestCelestrakClient_RateLimitHeader тестирует обработку 429
func TestCelestrakClient_RateLimitHeader(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		if attemptCount == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ISS (ZARYA)\n" + issLine1 + "\n" + issLine2))
	}))
	defer server.Close()

	client := NewCelestrakClient(
		WithBaseURL(server.URL),
		WithRateLimit(0),
		WithMaxRetries(2),
	)

	ctx := context.Background()
	tle, err := client.FetchByNoradID(ctx, 25544)
	if err != nil {
		t.Fatalf("FetchByNoradID() error = %v", err)
	}

	if tle.NoradID != 25544 {
		t.Errorf("NoradID = %d, want 25544", tle.NoradID)
	}
}
