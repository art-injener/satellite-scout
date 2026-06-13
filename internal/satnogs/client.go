package satnogs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Константы клиента SatNOGS DB API.
const (
	// DefaultBaseURL — корень публичного SatNOGS DB API.
	DefaultBaseURL = "https://db.satnogs.org/api"

	// DefaultRateLimit — минимальный интервал между запросами к API.
	// SatNOGS не публикует жёсткий лимит, но 1 секунда — вежливая верхняя граница
	// для нашего паттерна fetch on first sight (десятки уникальных NORAD в час).
	DefaultRateLimit = 1 * time.Second

	// DefaultTimeout — таймаут одного HTTP-запроса.
	// SatNOGS DB иногда отвечает медленно, но держать воркер по 30 секунд на
	// каждом «висящем» спутнике слишком дорого. 12 секунд — компромисс: успеваем
	// получить ответ в обычной ситуации и быстро отпускаем воркер при зависании.
	DefaultTimeout = 12 * time.Second

	// DefaultMaxRetries — количество дополнительных попыток при 5xx / 429.
	// Таймауты и отмена контекста НЕ ретраятся на уровне клиента: повтор медленного
	// запроса почти никогда не помогает и блокирует воркер. Повтор после таймаута
	// берёт на себя сервисный слой (см. Service.retryAfter).
	DefaultMaxRetries = 2

	// userAgent — постоянный User-Agent для идентификации проекта в логах SatNOGS.
	userAgent = "Satellite Scout/1.0 (https://github.com/art-injener/satellite-scout)"
)

// Sentinel-ошибки клиента SatNOGS.
// Использовать через errors.Is(err, satnogs.ErrSatNOGSNotFound).
var (
	ErrSatNOGSNotFound         = errors.New("satnogs: resource not found")
	ErrSatNOGSBadRequest       = errors.New("satnogs: bad request (400)")
	ErrSatNOGSRateLimit        = errors.New("satnogs: rate limited (429)")
	ErrSatNOGSClientError      = errors.New("satnogs: client error (4xx)")
	ErrSatNOGSServerError      = errors.New("satnogs: server error")
	ErrSatNOGSUnexpectedStatus = errors.New("satnogs: unexpected HTTP status")
	ErrSatNOGSDecode           = errors.New("satnogs: failed to decode response")
	ErrSatNOGSInvalidNorad     = errors.New("satnogs: invalid norad id")
)

// Client — HTTP-клиент SatNOGS DB API.
// Потокобезопасен: rate-limit держится через sync.Mutex.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	rateLimit   time.Duration
	maxRetries  int
	lastRequest time.Time
	mu          sync.Mutex
}

// Option — функциональная опция конфигурации клиента.
type Option func(*Client)

// WithHTTPClient подменяет http.Client (для тестов, прокси и т.п.).
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithRateLimit задаёт минимальный интервал между запросами.
// 0 — без задержки (полезно для тестов).
func WithRateLimit(d time.Duration) Option {
	return func(c *Client) {
		if d >= 0 {
			c.rateLimit = d
		}
	}
}

// WithTimeout задаёт таймаут одного HTTP-запроса.
// Неположительное значение игнорируется — остаётся DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 && c.httpClient != nil {
			c.httpClient.Timeout = d
		}
	}
}

// WithMaxRetries задаёт количество дополнительных попыток после первой неудачи.
func WithMaxRetries(n int) Option {
	return func(c *Client) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithBaseURL подменяет корневой URL API (используется в тестах с httptest.Server).
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		if baseURL != "" {
			c.baseURL = baseURL
		}
	}
}

// NewClient создаёт клиент SatNOGS с дефолтными значениями и применяет опции.
func NewClient(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: DefaultTimeout},
		baseURL:    DefaultBaseURL,
		rateLimit:  DefaultRateLimit,
		maxRetries: DefaultMaxRetries,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FetchTransmitters загружает список передатчиков для указанного NORAD ID.
// Возвращает пустой срез без ошибки, если у спутника нет ни одного зарегистрированного передатчика.
func (c *Client) FetchTransmitters(ctx context.Context, noradID int) ([]Transmitter, error) {
	if noradID <= 0 {
		return nil, fmt.Errorf("%w: %d", ErrSatNOGSInvalidNorad, noradID)
	}

	q := url.Values{}
	q.Set("satellite__norad_cat_id", strconv.Itoa(noradID))
	q.Set("format", "json")

	endpoint := fmt.Sprintf("%s/transmitters/?%s", c.baseURL, q.Encode())

	body, err := c.fetch(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("satnogs: fetching transmitters for norad %d: %w", noradID, err)
	}

	var transmitters []Transmitter
	if decodeErr := json.Unmarshal(body, &transmitters); decodeErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrSatNOGSDecode, decodeErr)
	}
	return transmitters, nil
}

// retrySchedule — фиксированные задержки между попытками.
// Индекс = номер retry (1-based): retry#1 → 2с, retry#2 → 5с, далее 10с (cap).
// Расписание короткое намеренно: ретраятся только быстрые ошибки (5xx/429),
// поэтому долго ждать смысла нет.
var retrySchedule = []time.Duration{
	2 * time.Second,
	5 * time.Second,
}

const retryScheduleCap = 10 * time.Second

// retryBackoff возвращает задержку перед retry по фиксированному расписанию.
func retryBackoff(attempt int) time.Duration {
	idx := attempt - 1
	if idx < 0 {
		return 0
	}
	if idx < len(retrySchedule) {
		return retrySchedule[idx]
	}
	return retryScheduleCap
}

// fetch выполняет HTTP-запрос с rate-limit и retry по фиксированному расписанию.
// Ретраятся только быстрые транзиентные ошибки — 429 и 5xx (до maxRetries раз,
// задержки 2с → 5с → 10с). Не ретраятся:
//   - 4xx (кроме 429) — терминальные клиентские ошибки;
//   - таймаут запроса и отмена контекста — повтор медленного запроса блокирует
//     воркер и почти никогда не помогает (повтор отдан сервисному слою).
func (c *Client) fetch(ctx context.Context, endpoint string) ([]byte, error) {
	c.waitForRateLimit()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := retryBackoff(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		body, err := c.doRequest(ctx, endpoint)
		if err == nil {
			return body, nil
		}
		lastErr = err

		// Ошибка, при которой повтор бесполезен, — выходим сразу.
		if isNonRetryable(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("after %d retries: %w", c.maxRetries, lastErr)
}

// isNonRetryable сообщает, что повторять запрос бесполезно:
//   - 4xx кроме 429 — терминальная клиентская ошибка;
//   - таймаут запроса / отмена контекста — повтор только зря держит воркер.
func isNonRetryable(err error) bool {
	return errors.Is(err, ErrSatNOGSNotFound) ||
		errors.Is(err, ErrSatNOGSBadRequest) ||
		errors.Is(err, ErrSatNOGSClientError) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled)
}

// waitForRateLimit блокирует горутину до соблюдения интервала между запросами.
// Один общий mutex на клиент → параллельные FetchTransmitters сериализуются.
func (c *Client) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.rateLimit <= 0 {
		c.lastRequest = time.Now()
		return
	}
	elapsed := time.Since(c.lastRequest)
	if elapsed < c.rateLimit {
		time.Sleep(c.rateLimit - elapsed)
	}
	c.lastRequest = time.Now()
}

// doRequest выполняет один HTTP-запрос и интерпретирует статус-код.
func (c *Client) doRequest(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	switch {
	case resp.StatusCode == http.StatusOK:
		// продолжаем чтение тела ниже
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrSatNOGSNotFound
	case resp.StatusCode == http.StatusBadRequest:
		return nil, fmt.Errorf("%w: %d", ErrSatNOGSBadRequest, resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, ErrSatNOGSRateLimit
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		// Любая другая 4xx (401, 403, …) — клиентская ошибка без ретрая.
		return nil, fmt.Errorf("%w: %d", ErrSatNOGSClientError, resp.StatusCode)
	case resp.StatusCode >= 500:
		return nil, fmt.Errorf("%w: %d", ErrSatNOGSServerError, resp.StatusCode)
	default:
		return nil, fmt.Errorf("%w: %d", ErrSatNOGSUnexpectedStatus, resp.StatusCode)
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("reading response: %w", readErr)
	}
	return body, nil
}
