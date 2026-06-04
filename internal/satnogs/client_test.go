package satnogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fixtureISS — фрагмент реального ответа SatNOGS для NORAD 25544.
const fixtureISS = `[
  {
    "uuid": "ZJxCeQmih9zDfYNVrB4wRN",
    "description": "Mode V APRS",
    "alive": true,
    "type": "Transceiver",
    "downlink_low": 145825000,
    "mode": "AFSK",
    "baud": 1200.0,
    "norad_cat_id": 25544,
    "status": "active"
  },
  {
    "uuid": "PjfcFc4PZ8M8n3thuyA6x9",
    "description": "Mode V/V FM",
    "alive": true,
    "type": "Transceiver",
    "downlink_low": 145800000,
    "mode": "FM",
    "norad_cat_id": 25544,
    "status": "active"
  }
]`

func TestClient_FetchTransmitters_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/transmitters/") {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("satellite__norad_cat_id"); got != "25544" {
			t.Errorf("query satellite__norad_cat_id = %q, want %q", got, "25544")
		}
		if got := r.URL.Query().Get("format"); got != "json" {
			t.Errorf("query format = %q, want %q", got, "json")
		}
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "Satellite Scout") {
			t.Errorf("User-Agent = %q, want to contain 'Satellite Scout'", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fixtureISS))
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0))
	ctx := context.Background()

	transmitters, err := c.FetchTransmitters(ctx, 25544)
	if err != nil {
		t.Fatalf("FetchTransmitters() error = %v", err)
	}
	if len(transmitters) != 2 {
		t.Fatalf("len(transmitters) = %d, want 2", len(transmitters))
	}
	if transmitters[0].UUID != "ZJxCeQmih9zDfYNVrB4wRN" {
		t.Errorf("UUID = %q, want %q", transmitters[0].UUID, "ZJxCeQmih9zDfYNVrB4wRN")
	}
	if transmitters[0].DownlinkLow == nil || *transmitters[0].DownlinkLow != 145825000 {
		t.Errorf("DownlinkLow = %v, want 145825000", transmitters[0].DownlinkLow)
	}
	if transmitters[0].Baud == nil || *transmitters[0].Baud != 1200.0 {
		t.Errorf("Baud = %v, want 1200.0", transmitters[0].Baud)
	}
}

func TestClient_FetchTransmitters_InvalidNoradID(t *testing.T) {
	c := NewClient(WithBaseURL("http://invalid"), WithRateLimit(0))
	if _, err := c.FetchTransmitters(context.Background(), 0); err == nil {
		t.Error("FetchTransmitters(0) expected error, got nil")
	}
	if _, err := c.FetchTransmitters(context.Background(), -1); err == nil {
		t.Error("FetchTransmitters(-1) expected error, got nil")
	}
}

func TestClient_FetchTransmitters_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(2))
	_, err := c.FetchTransmitters(context.Background(), 99999)
	if err == nil {
		t.Fatal("FetchTransmitters() expected error, got nil")
	}
	if !errors.Is(err, ErrSatNOGSNotFound) {
		t.Errorf("err = %v, want ErrSatNOGSNotFound", err)
	}
}

func TestClient_FetchTransmitters_NotFoundNoRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(3))
	_, err := c.FetchTransmitters(context.Background(), 99999)
	if !errors.Is(err, ErrSatNOGSNotFound) {
		t.Fatalf("err = %v, want ErrSatNOGSNotFound", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 404)", got)
	}
}

func TestClient_FetchTransmitters_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(0))
	_, err := c.FetchTransmitters(context.Background(), 25544)
	if err == nil || !errors.Is(err, ErrSatNOGSServerError) {
		t.Errorf("err = %v, want ErrSatNOGSServerError", err)
	}
}

func TestClient_FetchTransmitters_RateLimit429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(0))
	_, err := c.FetchTransmitters(context.Background(), 25544)
	if err == nil || !errors.Is(err, ErrSatNOGSRateLimit) {
		t.Errorf("err = %v, want ErrSatNOGSRateLimit", err)
	}
}

func TestClient_FetchTransmitters_RetriesOnServerError(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(3))
	transmitters, err := c.FetchTransmitters(context.Background(), 25544)
	if err != nil {
		t.Fatalf("FetchTransmitters() error = %v", err)
	}
	if len(transmitters) != 0 {
		t.Errorf("len = %d, want 0 (empty array)", len(transmitters))
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3 (retry on 5xx)", got)
	}
}

func TestClient_FetchTransmitters_RateLimitDelay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	rate := 100 * time.Millisecond
	c := NewClient(WithBaseURL(server.URL), WithRateLimit(rate), WithMaxRetries(0))
	ctx := context.Background()

	start := time.Now()
	for i := 0; i < 3; i++ {
		_, _ = c.FetchTransmitters(ctx, 25544)
	}
	elapsed := time.Since(start)

	// Между 3 запросами 2 паузы по rate.
	expectedMin := 2 * rate
	if elapsed < expectedMin {
		t.Errorf("elapsed = %v, expected at least %v", elapsed, expectedMin)
	}
}

func TestClient_FetchTransmitters_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(0))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := c.FetchTransmitters(ctx, 25544); err == nil {
		t.Error("FetchTransmitters() expected error on context cancel, got nil")
	}
}

func TestClient_FetchTransmitters_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(0))
	if _, err := c.FetchTransmitters(context.Background(), 25544); err == nil || !errors.Is(err, ErrSatNOGSDecode) {
		t.Errorf("err = %v, want ErrSatNOGSDecode", err)
	}
}

func TestClient_FetchTransmitters_EmptyArrayOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0))
	transmitters, err := c.FetchTransmitters(context.Background(), 25544)
	if err != nil {
		t.Fatalf("FetchTransmitters() error = %v", err)
	}
	if len(transmitters) != 0 {
		t.Errorf("len = %d, want 0", len(transmitters))
	}
}

func TestClient_FetchTransmitters_BadRequest400NoRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`"Enter a number."`))
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(3))
	_, err := c.FetchTransmitters(context.Background(), 25544)
	if err == nil {
		t.Fatal("FetchTransmitters() expected error, got nil")
	}
	if !errors.Is(err, ErrSatNOGSBadRequest) {
		t.Errorf("err = %v, want ErrSatNOGSBadRequest", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 400)", got)
	}
}

func TestClient_FetchTransmitters_OtherClientErrorNoRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusForbidden) // 403
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(3))
	_, err := c.FetchTransmitters(context.Background(), 25544)
	if err == nil {
		t.Fatal("FetchTransmitters() expected error, got nil")
	}
	if !errors.Is(err, ErrSatNOGSClientError) {
		t.Errorf("err = %v, want ErrSatNOGSClientError", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 403)", got)
	}
}

func TestClient_FetchTransmitters_429DoesRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	c := NewClient(WithBaseURL(server.URL), WithRateLimit(0), WithMaxRetries(3))
	transmitters, err := c.FetchTransmitters(context.Background(), 25544)
	if err != nil {
		t.Fatalf("FetchTransmitters() error = %v", err)
	}
	if len(transmitters) != 0 {
		t.Errorf("len = %d, want 0", len(transmitters))
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3 (429 should retry)", got)
	}
}

func TestClient_DefaultsApplied(t *testing.T) {
	c := NewClient()
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.rateLimit != DefaultRateLimit {
		t.Errorf("rateLimit = %v, want %v", c.rateLimit, DefaultRateLimit)
	}
	if c.maxRetries != DefaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", c.maxRetries, DefaultMaxRetries)
	}
	if c.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v", c.httpClient.Timeout, DefaultTimeout)
	}
}

func TestWithTimeout(t *testing.T) {
	c := NewClient(WithTimeout(3 * time.Second))
	if c.httpClient.Timeout != 3*time.Second {
		t.Errorf("httpClient.Timeout = %v, want 3s", c.httpClient.Timeout)
	}

	// Неположительное значение игнорируется — остаётся дефолт.
	c2 := NewClient(WithTimeout(0))
	if c2.httpClient.Timeout != DefaultTimeout {
		t.Errorf("httpClient.Timeout = %v, want %v (default)", c2.httpClient.Timeout, DefaultTimeout)
	}
}

func TestClient_FetchTransmitters_TimeoutNoRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		time.Sleep(300 * time.Millisecond) // дольше таймаута клиента
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Короткий таймаут + ретраи разрешены: убеждаемся, что таймаут НЕ повторяется.
	c := NewClient(
		WithBaseURL(server.URL),
		WithRateLimit(0),
		WithMaxRetries(3),
		WithTimeout(50*time.Millisecond),
	)

	_, err := c.FetchTransmitters(context.Background(), 25544)
	if err == nil {
		t.Fatal("FetchTransmitters() expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1 (timeout must not retry)", got)
	}
}

func TestRetryBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 0},
		{1, 2 * time.Second},
		{2, 5 * time.Second},
		{3, 10 * time.Second},
		{4, 10 * time.Second},
		{100, 10 * time.Second},
	}
	for _, tt := range tests {
		if got := retryBackoff(tt.attempt); got != tt.want {
			t.Errorf("retryBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}
