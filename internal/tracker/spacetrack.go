package tracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Константы Space-Track API.
const (
	// SpaceTrackBaseURL адрес площадки Space-Track.
	SpaceTrackBaseURL = "https://www.space-track.org"

	// DefaultSpaceTrackRateLimit пауза между запросами: лимит площадки 30 запросов в минуту.
	DefaultSpaceTrackRateLimit = 3 * time.Second

	// DefaultSpaceTrackBatchSize сколько NORAD в одном запросе GP, чтобы строка запроса оставалась короткой.
	DefaultSpaceTrackBatchSize = 100

	spaceTrackTimeout   = 60 * time.Second
	spaceTrackLoginPath = "/ajaxauth/login"
	spaceTrackGPPath    = "/basicspacedata/query/class/gp/NORAD_CAT_ID/%s/format/3le"

	// В формате 3le строка имени начинается с "0 ".
	spaceTrackNamePrefix = "0 "

	// На неверную учётку площадка отвечает кодом 200 и этим телом.
	spaceTrackLoginFailed = `"Login":"Failed"`

	scoutUserAgent = "Satellite Scout/1.0 (https://github.com/art-injener/satellite-scout)"
)

// Ошибки клиента Space-Track. Тексты ошибок не содержат учётку.
var (
	ErrSpaceTrackLogin  = errors.New("space-track: login failed")
	ErrSpaceTrackStatus = errors.New("space-track: unexpected HTTP status")
)

// SpaceTrackClient загружает элементы орбит с Space-Track по списку NORAD.
// Вход по cookie: сессия живёт около двух часов, после 401 клиент входит заново.
type SpaceTrackClient struct {
	httpClient *http.Client
	baseURL    string
	identity   string
	password   string
	rateLimit  time.Duration
	batchSize  int

	// Один проход за раз: сессия и пауза между запросами общие.
	mu          sync.Mutex
	loggedIn    bool
	lastRequest time.Time
}

// SpaceTrackOption функция настройки клиента Space-Track.
type SpaceTrackOption func(*SpaceTrackClient)

// WithSpaceTrackBaseURL устанавливает адрес площадки (для тестирования).
func WithSpaceTrackBaseURL(u string) SpaceTrackOption {
	return func(c *SpaceTrackClient) {
		c.baseURL = strings.TrimSuffix(u, "/")
	}
}

// WithSpaceTrackRateLimit устанавливает паузу между запросами.
func WithSpaceTrackRateLimit(d time.Duration) SpaceTrackOption {
	return func(c *SpaceTrackClient) {
		c.rateLimit = d
	}
}

// WithSpaceTrackBatchSize устанавливает число NORAD в одном запросе.
func WithSpaceTrackBatchSize(n int) SpaceTrackOption {
	return func(c *SpaceTrackClient) {
		if n > 0 {
			c.batchSize = n
		}
	}
}

// BatchSize возвращает число NORAD в одной пачке GP/SATCAT.
func (c *SpaceTrackClient) BatchSize() int {
	if c == nil || c.batchSize <= 0 {
		return DefaultSpaceTrackBatchSize
	}
	return c.batchSize
}

// NewSpaceTrackClient создаёт клиент Space-Track с учёткой оператора.
func NewSpaceTrackClient(identity, password string, opts ...SpaceTrackOption) *SpaceTrackClient {
	jar, _ := cookiejar.New(nil) //nolint:errcheck // без опций cookiejar.New ошибку не возвращает
	c := &SpaceTrackClient{
		httpClient: &http.Client{Timeout: spaceTrackTimeout, Jar: jar},
		baseURL:    SpaceTrackBaseURL,
		identity:   identity,
		password:   password,
		rateLimit:  DefaultSpaceTrackRateLimit,
		batchSize:  DefaultSpaceTrackBatchSize,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SpaceTrackProgress сообщает, сколько пачек уже выполнено из total в текущей фазе.
// nil — без индикации. Вызывается после каждой пачки, в том числе пустой (204).
type SpaceTrackProgress func(done, total int)

// FetchByNoradIDs загружает свежие элементы для списка NORAD пачками.
// Ошибка любой пачки обрывает проход: частичный результат не возвращается.
// NORAD, которых площадка не знает, просто отсутствуют в ответе.
func (c *SpaceTrackClient) FetchByNoradIDs(ctx context.Context, ids []int) ([]*TLE, error) {
	return c.fetchByNoradIDs(ctx, ids, nil)
}

// FetchByNoradIDsProgress то же, что FetchByNoradIDs, с колбэком прогресса по пачкам.
func (c *SpaceTrackClient) FetchByNoradIDsProgress(ctx context.Context, ids []int, progress SpaceTrackProgress) ([]*TLE, error) {
	return c.fetchByNoradIDs(ctx, ids, progress)
}

func (c *SpaceTrackClient) fetchByNoradIDs(ctx context.Context, ids []int, progress SpaceTrackProgress) ([]*TLE, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loggedIn {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}

	total := spaceTrackBatchCount(len(ids), c.batchSize)
	var all []*TLE
	done := 0
	for batch := range slices.Chunk(ids, c.batchSize) {
		tles, err := c.fetchBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		all = append(all, tles...)
		done++
		if progress != nil {
			progress(done, total)
		}
	}
	return all, nil
}

// spaceTrackBatchCount — сколько пачек уйдёт на n идентификаторов.
func spaceTrackBatchCount(n, batchSize int) int {
	if n <= 0 || batchSize <= 0 {
		return 0
	}
	return (n + batchSize - 1) / batchSize
}

// fetchBatch запрашивает элементы одной пачки.
func (c *SpaceTrackClient) fetchBatch(ctx context.Context, ids []int) ([]*TLE, error) {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	queryURL := c.baseURL + fmt.Sprintf(spaceTrackGPPath, strings.Join(parts, ","))

	body, status, err := c.query(ctx, queryURL)
	if err != nil {
		return nil, err
	}
	// Ни одного известного NORAD в пачке (например, временные номера будущих КА): площадка отвечает 204.
	if status == http.StatusNoContent {
		return nil, nil
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrSpaceTrackStatus, status)
	}

	tles, err := ParseTLEBatch(body)
	if err != nil {
		return nil, fmt.Errorf("space-track: "+errMsgParsingTLE, err)
	}
	for _, tle := range tles {
		tle.Name = strings.TrimPrefix(tle.Name, spaceTrackNamePrefix)
	}
	return tles, nil
}

// query выполняет запрос в открытой сессии; на истёкшую сессию входит заново один раз.
func (c *SpaceTrackClient) query(ctx context.Context, queryURL string) (string, int, error) {
	body, status, err := c.get(ctx, queryURL)
	if err == nil && status == http.StatusUnauthorized {
		c.loggedIn = false
		if loginErr := c.login(ctx); loginErr != nil {
			return "", 0, loginErr
		}
		body, status, err = c.get(ctx, queryURL)
	}
	return body, status, err
}

// login открывает сессию; cookie сохраняется в jar HTTP-клиента.
func (c *SpaceTrackClient) login(ctx context.Context) error {
	if err := c.waitForRateLimit(ctx); err != nil {
		return err
	}
	form := url.Values{"identity": {c.identity}, "password": {c.password}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+spaceTrackLoginPath,
		strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("space-track: creating login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", scoutUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("space-track: login request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("space-track: reading login response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: login %d", ErrSpaceTrackStatus, resp.StatusCode)
	}
	if strings.Contains(string(body), spaceTrackLoginFailed) {
		return ErrSpaceTrackLogin
	}
	c.loggedIn = true
	return nil
}

// get выполняет GET с паузой под лимит площадки и возвращает тело и код ответа.
func (c *SpaceTrackClient) get(ctx context.Context, u string) (string, int, error) {
	if err := c.waitForRateLimit(ctx); err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", 0, fmt.Errorf("space-track: creating request: %w", err)
	}
	req.Header.Set("User-Agent", scoutUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("space-track: query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("space-track: reading response: %w", err)
	}
	return string(body), resp.StatusCode, nil
}

// waitForRateLimit выдерживает паузу с прошлого запроса; отмена контекста прерывает ожидание.
func (c *SpaceTrackClient) waitForRateLimit(ctx context.Context) error {
	if !c.lastRequest.IsZero() {
		if d := c.rateLimit - time.Since(c.lastRequest); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	c.lastRequest = time.Now()
	return nil
}
