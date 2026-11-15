package satnogs

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// diskCacheFile — содержимое файла кеша передатчиков.
type diskCacheFile struct {
	Satellites map[int]diskCacheEntry `json:"satellites"`
}

type diskCacheEntry struct {
	FetchedAt    time.Time     `json:"fetched_at"`
	Transmitters []Transmitter `json:"transmitters"`
}

// WithDiskCache читает сохранённые передатчики из path и дальше сохраняет туда каждую
// удачную загрузку. После рестарта частоты есть сразу, даже если SatNOGS недоступен;
// устаревшие записи отдаются, пока идёт повторный запрос.
// Отсутствующий или битый файл не мешает работе: кеш начинается с пустого.
func (s *Service) WithDiskCache(path string) *Service {
	s.diskPath = path
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			s.logger.Warn("satnogs: failed to read disk cache", slog.String("path", path), slog.Any("error", err))
		}
		return s
	}
	var file diskCacheFile
	if err = json.Unmarshal(data, &file); err != nil {
		s.logger.Warn("satnogs: broken disk cache, starting empty", slog.String("path", path), slog.Any("error", err))
		return s
	}

	s.mu.Lock()
	for id, e := range file.Satellites {
		s.cache[id] = &cacheEntry{
			transmitters: e.Transmitters,
			primary:      SelectPrimary(e.Transmitters),
			fetchedAt:    e.FetchedAt,
			lastOK:       e.FetchedAt,
		}
	}
	s.mu.Unlock()
	s.logger.Info("satnogs: disk cache loaded", slog.String("path", path), slog.Int("satellites", len(file.Satellites)))
	return s
}

// saveDiskCache пишет в файл все удачные записи кеша. Запись атомарная: временный файл и rename.
func (s *Service) saveDiskCache() {
	if s.diskPath == "" {
		return
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()

	file := diskCacheFile{Satellites: make(map[int]diskCacheEntry)}
	s.mu.RLock()
	for id, e := range s.cache {
		if e.lastOK.IsZero() {
			continue
		}
		file.Satellites[id] = diskCacheEntry{FetchedAt: e.lastOK, Transmitters: e.transmitters}
	}
	s.mu.RUnlock()

	if err := writeFileAtomic(s.diskPath, file); err != nil {
		s.logger.Warn("satnogs: failed to save disk cache", slog.String("path", s.diskPath), slog.Any("error", err))
	}
}

func writeFileAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
