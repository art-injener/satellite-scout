package freqplan

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// DefaultAMSATURL — зеркало JE9PEL/AMSAT (только active).
const DefaultAMSATURL = "https://raw.githubusercontent.com/palewire/amateur-satellite-database/main/data/amsat-active-frequencies.csv"

// DefaultAMSATTTL — как у SatNOGS: сутки.
const DefaultAMSATTTL = 24 * time.Hour

// HTTPDoer — узкий клиент для тестов.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// LoadAMSATFile читает уже скачанный CSV. Нет файла — пустой источник.
func LoadAMSATFile(path string) (*AMSAT, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AMSAT{byNorad: map[int][]Tx{}}, nil
		}
		return nil, fmt.Errorf("freqplan: amsat open: %w", err)
	}
	defer f.Close()
	return ParseAMSAT(f)
}

// FetchAMSAT обновляет cachePath с URL, если кеш старше ttl или отсутствует.
// При ошибке сети отдаёт прежний кеш; если кеша нет — пустой источник.
func FetchAMSAT(ctx context.Context, client HTTPDoer, url, cachePath string, ttl time.Duration) (*AMSAT, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if url == "" {
		url = DefaultAMSATURL
	}
	if ttl <= 0 {
		ttl = DefaultAMSATTTL
	}
	if cacheFresh(cachePath, ttl) {
		return LoadAMSATFile(cachePath)
	}
	if err := downloadAMSAT(ctx, client, url, cachePath); err != nil {
		if cached, loadErr := LoadAMSATFile(cachePath); loadErr == nil {
			return cached, nil
		}
		return &AMSAT{byNorad: map[int][]Tx{}}, nil
	}
	return LoadAMSATFile(cachePath)
}

func cacheFresh(path string, ttl time.Duration) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return time.Since(st.ModTime()) < ttl
}

func downloadAMSAT(ctx context.Context, client HTTPDoer, url, cachePath string) error {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("freqplan: amsat http %d", resp.StatusCode)
	}
	tmp := cachePath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, cachePath)
}
