package freqplan

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultHeavensAboveTTL — страница покрывает сутки пролётов; robots.txt просит не чаще раза в 10 с.
const DefaultHeavensAboveTTL = 12 * time.Hour

// maxHeavensAbovePage — предел размера страницы (обычно около 70 КБ).
const maxHeavensAbovePage = 4 << 20

// HeavensAboveURL — страница любительских КА для координат станции, подписи на английском.
func HeavensAboveURL(lat, lon float64) string {
	return fmt.Sprintf(
		"https://heavens-above.com/AmateurSats.aspx?lat=%.4f&lng=%.4f&loc=Unnamed&alt=0&tz=UCT&cul=en", lat, lon)
}

// FetchHeavensAbove обновляет кеш cachePath с url, если он старше ttl или отсутствует.
// Страница без таблицы пролётов кеш не затирает. При ошибке сети отдаёт прежний кеш;
// если кеша нет — пустой источник и ошибку.
func FetchHeavensAbove(
	ctx context.Context, client HTTPDoer, url, cachePath string, ttl time.Duration,
) (*HeavensAbove, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if ttl <= 0 {
		ttl = DefaultHeavensAboveTTL
	}
	if cacheFresh(cachePath, ttl) {
		if src, err := loadHeavensAboveFile(cachePath); err == nil {
			return src, nil
		}
	}
	src, err := downloadHeavensAbove(ctx, client, url, cachePath)
	if err == nil {
		return src, nil
	}
	if cached, loadErr := loadHeavensAboveFile(cachePath); loadErr == nil {
		return cached, nil
	}
	return &HeavensAbove{byNorad: map[int][]Tx{}}, err
}

func loadHeavensAboveFile(path string) (*HeavensAbove, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// Кеш пишется целиком при загрузке, поэтому mtime — момент загрузки страницы.
	return ParseHeavensAbove(f, info.ModTime())
}

// downloadHeavensAbove качает страницу, проверяет разбором и только потом пишет в кеш.
func downloadHeavensAbove(ctx context.Context, client HTTPDoer, url, cachePath string) (*HeavensAbove, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("freqplan: heavens-above: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrHeavensAboveHTTP, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHeavensAbovePage))
	if err != nil {
		return nil, fmt.Errorf("freqplan: heavens-above read: %w", err)
	}
	src, err := ParseHeavensAbove(bytes.NewReader(body), time.Now())
	if err != nil {
		return nil, err
	}
	// Не записался кеш — страница всё равно годна, в следующий раз скачаем снова.
	if err = writeCacheFile(cachePath, body); err != nil {
		slog.WarnContext(ctx, "freqplan: heavens-above cache not saved",
			slog.String("path", cachePath), slog.Any("error", err))
	}
	return src, nil
}

func writeCacheFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
