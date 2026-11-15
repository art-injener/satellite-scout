package satnogs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// DiskEntry передатчики одного КА из файла кеша и время их загрузки.
type DiskEntry struct {
	FetchedAt    time.Time
	Transmitters []Transmitter
}

// ReadDiskCache читает файл кеша передатчиков без запуска сервиса (для реестра КА).
// Нет файла — пустой результат без ошибки.
func ReadDiskCache(path string) (map[int]DiskEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return map[int]DiskEntry{}, nil
		}
		return nil, fmt.Errorf("satnogs: reading disk cache: %w", err)
	}
	var file diskCacheFile
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("satnogs: decoding disk cache: %w", err)
	}
	out := make(map[int]DiskEntry, len(file.Satellites))
	for id, e := range file.Satellites {
		out[id] = DiskEntry(e)
	}
	return out, nil
}
