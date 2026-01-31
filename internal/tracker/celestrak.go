// Package tracker реализует парсинг TLE и расчёт позиций спутников.
package tracker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Константы Celestrak API
const (
	// CelestrakBaseURL базовый URL Celestrak API
	CelestrakBaseURL = "https://celestrak.org/NORAD/elements/gp.php"

	// DefaultRateLimit минимальный интервал между запросами (рекомендация Celestrak)
	DefaultRateLimit = 2 * time.Second

	// DefaultTimeout таймаут HTTP запроса
	DefaultTimeout = 30 * time.Second

	// DefaultMaxRetries количество повторных попыток
	DefaultMaxRetries = 3
)

// Ошибки Celestrak клиента
var (
	ErrCelestrakNotFound    = fmt.Errorf("satellite not found")
	ErrCelestrakRateLimit   = fmt.Errorf("rate limited (429)")
	ErrCelestrakServerError = fmt.Errorf("server error")
)

// SatelliteGroup предустановленные группы спутников Celestrak.
type SatelliteGroup string

// Предустановленные группы спутников
const (
	GroupStations          SatelliteGroup = "stations"     // МКС и связанные
	GroupWeather           SatelliteGroup = "weather"      // Метеорологические
	GroupNOAA              SatelliteGroup = "noaa"         // NOAA спутники
	GroupGOES              SatelliteGroup = "goes"         // GOES спутники
	GroupAmateur           SatelliteGroup = "amateur"      // Радиолюбительские
	GroupCubesat           SatelliteGroup = "cubesat"      // CubeSat
	GroupStarlink          SatelliteGroup = "starlink"     // Starlink
	GroupOneWeb            SatelliteGroup = "oneweb"       // OneWeb
	GroupGPS               SatelliteGroup = "gps-ops"      // GPS операционные
	GroupGlonass           SatelliteGroup = "glo-ops"      // GLONASS операционные
	GroupGalileo           SatelliteGroup = "galileo"      // Galileo
	GroupBeidou            SatelliteGroup = "beidou"       // BeiDou
	GroupSBASSatellites    SatelliteGroup = "sbas"         // SBAS
	GroupScienceSatellites SatelliteGroup = "science"      // Научные
	GroupGeostationary     SatelliteGroup = "geo"          // Геостационарные
	GroupIridium           SatelliteGroup = "iridium"      // Iridium
	GroupIridiumNEXT       SatelliteGroup = "iridium-NEXT" // Iridium NEXT
	GroupGlobalstar        SatelliteGroup = "globalstar"   // Globalstar
	GroupOrbcomm           SatelliteGroup = "orbcomm"      // Orbcomm
	GroupActive            SatelliteGroup = "active"       // Все активные спутники
	GroupAnalyst           SatelliteGroup = "analyst"      // Интересные объекты
	GroupMilitary          SatelliteGroup = "military"     // Военные
	GroupRadar             SatelliteGroup = "radar"        // Радарные
	GroupArgos             SatelliteGroup = "argos"        // ARGOS
	GroupPlanet            SatelliteGroup = "planet"       // Planet Labs
	GroupSpire             SatelliteGroup = "spire"        // Spire Global
	GroupResource          SatelliteGroup = "resource"     // Earth Resources
	GroupSARSat            SatelliteGroup = "sarsat"       // Search & Rescue
	GroupDMC               SatelliteGroup = "dmc"          // Disaster Monitoring
	GroupTDRSS             SatelliteGroup = "tdrss"        // Tracking & Data Relay
	GroupEducation         SatelliteGroup = "education"    // Образовательные
	GroupGeodetic          SatelliteGroup = "geodetic"     // Геодезические
	GroupEngineering       SatelliteGroup = "engineering"  // Инженерные
	GroupLastLaunch        SatelliteGroup = "tle-new"      // Последние запуски
)

// CelestrakClient HTTP клиент для загрузки TLE с Celestrak.
type CelestrakClient struct {
	httpClient  *http.Client
	baseURL     string
	rateLimit   time.Duration
	maxRetries  int
	lastRequest time.Time
	mu          sync.Mutex
}

// CelestrakOption функция настройки клиента.
type CelestrakOption func(*CelestrakClient)

// WithHTTPClient устанавливает кастомный HTTP клиент.
func WithHTTPClient(client *http.Client) CelestrakOption {
	return func(c *CelestrakClient) {
		c.httpClient = client
	}
}

// WithRateLimit устанавливает интервал между запросами.
func WithRateLimit(d time.Duration) CelestrakOption {
	return func(c *CelestrakClient) {
		c.rateLimit = d
	}
}

// WithMaxRetries устанавливает количество повторных попыток.
func WithMaxRetries(n int) CelestrakOption {
	return func(c *CelestrakClient) {
		c.maxRetries = n
	}
}

// WithBaseURL устанавливает базовый URL (для тестирования).
func WithBaseURL(url string) CelestrakOption {
	return func(c *CelestrakClient) {
		c.baseURL = url
	}
}

// NewCelestrakClient создаёт новый клиент Celestrak.
func NewCelestrakClient(opts ...CelestrakOption) *CelestrakClient {
	c := &CelestrakClient{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
		},
		baseURL:    CelestrakBaseURL,
		rateLimit:  DefaultRateLimit,
		maxRetries: DefaultMaxRetries,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// FetchByNoradID загружает TLE по NORAD ID.
func (c *CelestrakClient) FetchByNoradID(ctx context.Context, noradID int) (*TLE, error) {
	url := fmt.Sprintf("%s?CATNR=%d&FORMAT=TLE", c.baseURL, noradID)

	data, err := c.fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching NORAD ID %d: %w", noradID, err)
	}

	tles, err := ParseTLEBatch(data)
	if err != nil {
		return nil, fmt.Errorf("parsing TLE: %w", err)
	}

	if len(tles) == 0 {
		return nil, fmt.Errorf("%w: NORAD ID %d", ErrCelestrakNotFound, noradID)
	}

	return tles[0], nil
}

// FetchGroup загружает TLE для группы спутников.
func (c *CelestrakClient) FetchGroup(ctx context.Context, group SatelliteGroup) ([]*TLE, error) {
	url := fmt.Sprintf("%s?GROUP=%s&FORMAT=TLE", c.baseURL, group)

	data, err := c.fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching group %s: %w", group, err)
	}

	tles, err := ParseTLEBatch(data)
	if err != nil {
		return nil, fmt.Errorf("parsing TLEs: %w", err)
	}

	return tles, nil
}

// FetchURL загружает TLE по произвольному URL.
func (c *CelestrakClient) FetchURL(ctx context.Context, url string) ([]*TLE, error) {
	data, err := c.fetch(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetching URL %s: %w", url, err)
	}

	tles, err := ParseTLEBatch(data)
	if err != nil {
		return nil, fmt.Errorf("parsing TLEs: %w", err)
	}

	return tles, nil
}

// FetchMultipleGroups загружает TLE для нескольких групп параллельно.
func (c *CelestrakClient) FetchMultipleGroups(ctx context.Context, groups []SatelliteGroup) ([]*TLE, error) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allTLEs []*TLE
		errors  []error
	)

	for _, group := range groups {
		wg.Add(1)
		go func(g SatelliteGroup) {
			defer wg.Done()

			tles, err := c.FetchGroup(ctx, g)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errors = append(errors, fmt.Errorf("group %s: %w", g, err))
				return
			}
			allTLEs = append(allTLEs, tles...)
		}(group)
	}

	wg.Wait()

	if len(errors) > 0 {
		return allTLEs, fmt.Errorf("errors fetching groups: %v", errors)
	}

	return allTLEs, nil
}

// fetch выполняет HTTP запрос с rate limiting и retry.
func (c *CelestrakClient) fetch(ctx context.Context, url string) (string, error) {
	c.waitForRateLimit()

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoff):
			}
		}

		data, err := c.doRequest(ctx, url)
		if err == nil {
			return data, nil
		}

		lastErr = err

		// Не повторяем при 404
		if err == ErrCelestrakNotFound {
			return "", err
		}
	}

	return "", fmt.Errorf("after %d retries: %w", c.maxRetries, lastErr)
}

// waitForRateLimit ждёт соблюдения rate limit.
func (c *CelestrakClient) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.lastRequest)
	if elapsed < c.rateLimit {
		time.Sleep(c.rateLimit - elapsed)
	}
	c.lastRequest = time.Now()
}

// doRequest выполняет один HTTP запрос.
func (c *CelestrakClient) doRequest(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("User-Agent", "SatWatch/1.0 (https://github.com/art-injener/satwatch-go)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// OK
	case http.StatusNotFound:
		return "", ErrCelestrakNotFound
	case http.StatusTooManyRequests:
		return "", ErrCelestrakRateLimit
	default:
		if resp.StatusCode >= 500 {
			return "", fmt.Errorf("%w: %d", ErrCelestrakServerError, resp.StatusCode)
		}
		return "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	// Celestrak возвращает "No GP data found" при отсутствии данных
	if string(body) == "No GP data found" {
		return "", ErrCelestrakNotFound
	}

	return string(body), nil
}

// GetGroupURL возвращает URL для загрузки группы.
func GetGroupURL(group SatelliteGroup) string {
	return fmt.Sprintf("%s?GROUP=%s&FORMAT=TLE", CelestrakBaseURL, group)
}

// GetNoradURL возвращает URL для загрузки по NORAD ID.
func GetNoradURL(noradID int) string {
	return fmt.Sprintf("%s?CATNR=%d&FORMAT=TLE", CelestrakBaseURL, noradID)
}

// AvailableGroups возвращает список всех предустановленных групп.
func AvailableGroups() []SatelliteGroup {
	return []SatelliteGroup{
		GroupStations, GroupWeather, GroupNOAA, GroupGOES,
		GroupAmateur, GroupCubesat, GroupStarlink, GroupOneWeb,
		GroupGPS, GroupGlonass, GroupGalileo, GroupBeidou,
		GroupSBASSatellites, GroupScienceSatellites, GroupGeostationary,
		GroupIridium, GroupIridiumNEXT, GroupGlobalstar, GroupOrbcomm,
		GroupActive, GroupAnalyst, GroupMilitary, GroupRadar,
		GroupArgos, GroupPlanet, GroupSpire, GroupResource,
		GroupSARSat, GroupDMC, GroupTDRSS, GroupEducation,
		GroupGeodetic, GroupEngineering, GroupLastLaunch,
	}
}
