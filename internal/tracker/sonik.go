package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"
)

const (
	// SonikCatalogURL каталог КА sonik.space (схема SatNOGS DB).
	SonikCatalogURL = "https://sonik.space/satellites/data/"

	// Каталог около 700 КБ отдаётся за 7–8 с.
	sonikTimeout = 60 * time.Second

	sonikStatusInOrbit = "in orbit"
	sonikStatusFuture  = "future"
)

// Ошибки клиента sonik.
var (
	ErrSonikStatus = errors.New("sonik: unexpected HTTP status")
	ErrSonikEmpty  = errors.New("sonik: catalog has no active satellites")
)

// SonikClient загружает каталог КА с sonik.space. Элементов орбит у sonik нет.
type SonikClient struct {
	httpClient *http.Client
	url        string
}

// SonikOption функция настройки клиента sonik.
type SonikOption func(*SonikClient)

// WithSonikURL устанавливает адрес каталога (для тестирования).
func WithSonikURL(u string) SonikOption {
	return func(c *SonikClient) {
		c.url = u
	}
}

// NewSonikClient создаёт клиент каталога sonik.
func NewSonikClient(opts ...SonikOption) *SonikClient {
	c := &SonikClient{
		httpClient: &http.Client{Timeout: sonikTimeout},
		url:        SonikCatalogURL,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SonikSat запись каталога sonik для реестра КА.
type SonikSat struct {
	NoradID  int
	Name     string
	Status   string // in orbit, future, re-entered
	CallSign string
	Operator string
	Launched string // дата запуска ГГГГ-ММ-ДД, пусто если неизвестна
}

// FetchNoradIDs возвращает NORAD КА на орбите и готовящихся к запуску, по возрастанию, без повторов.
// У будущих КА номера временные (99xxx): Space-Track их не знает, элементы появятся с настоящим номером.
func (c *SonikClient) FetchNoradIDs(ctx context.Context) ([]int, error) {
	sats, err := c.FetchCatalog(ctx)
	if err != nil {
		return nil, err
	}
	return SonikActiveIDs(sats)
}

// SonikActiveIDs выбирает из каталога NORAD КА на орбите и будущих, по возрастанию, без повторов.
func SonikActiveIDs(sats []SonikSat) ([]int, error) {
	ids := make([]int, 0, len(sats))
	for _, sat := range sats {
		if sat.Status == sonikStatusInOrbit || sat.Status == sonikStatusFuture {
			ids = append(ids, sat.NoradID)
		}
	}
	if len(ids) == 0 {
		return nil, ErrSonikEmpty
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

// Сколько суток после запуска КА из sonik ещё попадает в состав каталога Space-Track.
const sonikFreshLaunchDays = 60

// SonikFreshLaunchIDs — NORAD свежих запусков для состава каталога: статус in orbit или future,
// дата запуска не старше sonikFreshLaunchDays. Без даты или с нечитаемой датой не включаем.
func SonikFreshLaunchIDs(sats []SonikSat, now time.Time) []int {
	cutoff := now.AddDate(0, 0, -sonikFreshLaunchDays)
	ids := make([]int, 0)
	for _, sat := range sats {
		if sat.NoradID <= 0 {
			continue
		}
		if sat.Status != sonikStatusInOrbit && sat.Status != sonikStatusFuture {
			continue
		}
		if sat.Launched == "" {
			continue
		}
		launched, err := time.Parse("2006-01-02", sat.Launched)
		if err != nil {
			continue
		}
		if launched.Before(cutoff) {
			continue
		}
		ids = append(ids, sat.NoradID)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// FetchCatalog возвращает весь каталог sonik со всеми статусами, по возрастанию NORAD.
// Записи без NORAD пропускаются.
func (c *SonikClient) FetchCatalog(ctx context.Context) ([]SonikSat, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("sonik: creating request: %w", err)
	}
	req.Header.Set("User-Agent", scoutUserAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sonik: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrSonikStatus, resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			NoradCatID int    `json:"norad_cat_id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			CallSign   string `json:"call_sign"`
			Operator   string `json:"operator"`
			Launched   string `json:"launched"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("sonik: decoding catalog: %w", err)
	}

	sats := make([]SonikSat, 0, len(payload.Data))
	for _, sat := range payload.Data {
		if sat.NoradCatID <= 0 {
			continue
		}
		sats = append(sats, SonikSat{
			NoradID: sat.NoradCatID, Name: sat.Name, Status: sat.Status,
			CallSign: sat.CallSign, Operator: sat.Operator, Launched: sat.Launched,
		})
	}
	slices.SortStableFunc(sats, func(a, b SonikSat) int { return a.NoradID - b.NoradID })
	return sats, nil
}
