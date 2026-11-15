package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Только номер и дата схода: полный ответ SATCAT в 12 раз больше (замер на 100 КА: 46 КБ против 3,8 КБ).
const spaceTrackSatcatPath = "/basicspacedata/query/class/satcat/NORAD_CAT_ID/%s/predicates/NORAD_CAT_ID,DECAY/format/json"

// Формат даты DECAY в SATCAT.
const satcatDateLayout = "2006-01-02"

// SatcatRecord строка каталога SATCAT: NORAD и дата схода с орбиты.
type SatcatRecord struct {
	NoradID int
	Decay   time.Time // нулевая, если КА на орбите
}

// FetchSatcat загружает даты схода с орбиты для списка NORAD пачками, в той же сессии, что и GP.
// Возвращает записи и суммарный объём ответов в байтах.
// Ошибка пачки останавливает проход, но уже полученные записи возвращаются вместе с ошибкой.
func (c *SpaceTrackClient) FetchSatcat(ctx context.Context, ids []int) ([]SatcatRecord, int64, error) {
	return c.fetchSatcat(ctx, ids, nil)
}

// FetchSatcatProgress то же, что FetchSatcat, с колбэком прогресса по пачкам.
func (c *SpaceTrackClient) FetchSatcatProgress(ctx context.Context, ids []int, progress SpaceTrackProgress) ([]SatcatRecord, int64, error) {
	return c.fetchSatcat(ctx, ids, progress)
}

func (c *SpaceTrackClient) fetchSatcat(ctx context.Context, ids []int, progress SpaceTrackProgress) ([]SatcatRecord, int64, error) {
	if len(ids) == 0 {
		return nil, 0, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.loggedIn {
		if err := c.login(ctx); err != nil {
			return nil, 0, err
		}
	}

	batches := spaceTrackBatchCount(len(ids), c.batchSize)
	var (
		all   []SatcatRecord
		bytes int64
	)
	done := 0
	for batch := range slices.Chunk(ids, c.batchSize) {
		recs, n, err := c.fetchSatcatBatch(ctx, batch)
		if err != nil {
			// Уже полученные пачки не выбрасываем: отказ одной не стирает остальные.
			return all, bytes, err
		}
		all = append(all, recs...)
		bytes += n
		done++
		if progress != nil {
			progress(done, batches)
		}
	}
	return all, bytes, nil
}

func (c *SpaceTrackClient) fetchSatcatBatch(ctx context.Context, ids []int) ([]SatcatRecord, int64, error) {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	queryURL := c.baseURL + fmt.Sprintf(spaceTrackSatcatPath, strings.Join(parts, ","))
	body, status, err := c.query(ctx, queryURL)
	if err != nil || (status != http.StatusOK && status != http.StatusNoContent) {
		// Повтор один раз: зависшее соединение площадка обычно отпускает со следующей попытки.
		body, status, err = c.query(ctx, queryURL)
	}
	if err != nil {
		return nil, 0, err
	}
	if status == http.StatusNoContent {
		return nil, 0, nil
	}
	if status != http.StatusOK {
		return nil, 0, fmt.Errorf("%w: satcat %d", ErrSpaceTrackStatus, status)
	}

	var rows []struct {
		NoradCatID int     `json:"NORAD_CAT_ID,string"`
		Decay      *string `json:"DECAY"`
	}
	if err = json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, 0, fmt.Errorf("space-track: decoding satcat: %w", err)
	}
	recs := make([]SatcatRecord, 0, len(rows))
	for _, row := range rows {
		rec := SatcatRecord{NoradID: row.NoradCatID}
		if row.Decay != nil && *row.Decay != "" {
			rec.Decay, err = time.Parse(satcatDateLayout, *row.Decay)
			if err != nil {
				return nil, 0, fmt.Errorf("space-track: satcat decay of %d: %w", row.NoradCatID, err)
			}
		}
		recs = append(recs, rec)
	}
	return recs, int64(len(body)), nil
}
