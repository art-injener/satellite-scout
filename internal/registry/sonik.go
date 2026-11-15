package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// SonikTransmittersURL все передатчики sonik.space одним ответом (около 1,4 МБ, 5300 записей).
	SonikTransmittersURL = "https://sonik.space/transmitters/data/"

	// DefaultSonikTxTTL сколько живёт снимок передатчиков на диске до новой загрузки.
	DefaultSonikTxTTL = 24 * time.Hour

	sonikTxTimeout = 60 * time.Second
	sonikUserAgent = "Satellite Scout/1.0 (https://github.com/art-injener/satellite-scout)"
)

// ErrSonikTxStatus неожиданный код ответа sonik.
var ErrSonikTxStatus = errors.New("sonik: unexpected HTTP status")

// SonikTxClient загружает передатчики sonik.space и хранит сырой снимок ответа на диске.
type SonikTxClient struct {
	httpClient *http.Client
	url        string
}

// SonikTxOption функция настройки клиента передатчиков sonik.
type SonikTxOption func(*SonikTxClient)

// WithSonikTxURL устанавливает адрес выгрузки (для тестирования).
func WithSonikTxURL(u string) SonikTxOption {
	return func(c *SonikTxClient) {
		c.url = u
	}
}

// NewSonikTxClient создаёт клиент передатчиков sonik.
func NewSonikTxClient(opts ...SonikTxOption) *SonikTxClient {
	c := &SonikTxClient{
		httpClient: &http.Client{Timeout: sonikTxTimeout},
		url:        SonikTransmittersURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Load отдаёт передатчики и время снимка. Снимок на диске моложе ttl читается без сети.
// Иначе выгрузка скачивается и, если разобралась, заменяет снимок. При отказе сети или битом ответе
// отдаётся прежний снимок вместе с ошибкой; нет и снимка — только ошибка.
func (c *SonikTxClient) Load(
	ctx context.Context,
	cachePath string,
	ttl time.Duration,
) ([]Transmitter, time.Time, error) {
	if info, err := os.Stat(cachePath); err == nil && time.Since(info.ModTime()) < ttl {
		txs, at, readErr := readSonikTxFile(cachePath)
		if readErr == nil {
			return txs, at, nil
		}
	}

	now := time.Now()
	body, err := c.fetch(ctx)
	var txs []Transmitter
	if err == nil {
		txs, err = ParseSonikTransmitters(body, now)
	}
	if err == nil {
		if writeErr := writeFileAtomic(cachePath, body); writeErr != nil {
			return txs, now, fmt.Errorf("sonik: saving snapshot: %w", writeErr)
		}
		return txs, now, nil
	}

	cached, at, readErr := readSonikTxFile(cachePath)
	if readErr != nil {
		return nil, time.Time{}, err
	}
	return cached, at, err
}

func (c *SonikTxClient) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("sonik: creating request: %w", err)
	}
	req.Header.Set("User-Agent", sonikUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sonik: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrSonikTxStatus, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sonik: reading transmitters: %w", err)
	}
	return body, nil
}

// sonikNorad номер КА в выгрузке sonik: число или пустая строка у передатчиков без КА.
type sonikNorad int

func (n *sonikNorad) UnmarshalJSON(b []byte) error {
	v, err := strconv.Atoi(strings.Trim(string(b), `"`))
	if err != nil {
		*n = 0
		return nil //nolint:nilerr // пустая строка и null значат «КА не указан», запись пропускается
	}
	*n = sonikNorad(v)
	return nil
}

// ParseSonikTransmitters разбирает выгрузку sonik /transmitters/data/.
// Схема близка к SatNOGS, но NORAD в sat_norad, модуляция в downlink_mode, признака alive и uplink нет.
// Передатчики без NORAD пропускаются.
func ParseSonikTransmitters(data []byte, at time.Time) ([]Transmitter, error) {
	var payload struct {
		Data []struct {
			SatNorad    sonikNorad `json:"sat_norad"`
			Description string     `json:"description"`
			DownlinkLow *int64     `json:"downlink_low"`
			Mode        string     `json:"downlink_mode"`
			Status      string     `json:"status"`
			UUID        string     `json:"uuid"`
			Baud        *float64   `json:"baud"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("sonik: decoding transmitters: %w", err)
	}
	out := make([]Transmitter, 0, len(payload.Data))
	for _, row := range payload.Data {
		if row.SatNorad <= 0 {
			continue
		}
		out = append(out, Transmitter{
			UUID: row.UUID, NoradID: int(row.SatNorad), Name: row.Description,
			DownlinkHz: deref(row.DownlinkLow), Mode: row.Mode, Baud: deref(row.Baud),
			Status: row.Status, Source: SourceSonik, FetchedAt: at,
		})
	}
	return out, nil
}

func readSonikTxFile(path string) ([]Transmitter, time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	txs, err := ParseSonikTransmitters(data, info.ModTime())
	if err != nil {
		return nil, time.Time{}, err
	}
	return txs, info.ModTime(), nil
}

func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
