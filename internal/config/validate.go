package config

import (
	"fmt"
	"strings"
)

// ValidationError — одиночная ошибка валидации поля. Поле адресуется dotted-path
// (например "station.observer.lat") — это удобно фронту для подсветки нужного
// инпута в форме настроек.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors — агрегированная ошибка валидации. Реализует error и
// предоставляет JSON-сериализацию массива.
type ValidationErrors []ValidationError

func (errs ValidationErrors) Error() string {
	parts := make([]string, len(errs))
	for i, e := range errs {
		parts[i] = e.Error()
	}
	return "config validation failed: " + strings.Join(parts, "; ")
}

// HasErrors сообщает, есть ли в наборе хоть одна ошибка валидации.
func (errs ValidationErrors) HasErrors() bool {
	return len(errs) > 0
}

// Validate проверяет инвариант конфигурации и возвращает все нарушения сразу.
// Не модифицирует конфиг — это чистая валидация для PUT /api/settings.
//
// Покрытие:
//   - server.port не пустой;
//   - station.observer.lat ∈ [-90; 90], lon ∈ [-180; 180], alt_m ∈ [0; 8000];
//   - station.radio_paths: пустой список допустим (конфигурация "basic" —
//     только отслеживание спутников без SDR-оборудования); при наличии трактов
//     проверяются уникальные id, корректный freq_range_mhz и параметры поворотки;
//   - tle.update_interval > 0, satnogs.cache_ttl > 0;
//   - exclude_norad_file не пустой.
func (c *Config) Validate() error {
	var errs ValidationErrors

	if strings.TrimSpace(c.Server.Port) == "" {
		errs = append(errs, ValidationError{
			Field: "server.port", Message: "порт не может быть пустым",
		})
	}

	obs := c.Station.Observer
	if obs.Lat < -90 || obs.Lat > 90 {
		errs = append(errs, ValidationError{
			Field:   "station.observer.lat",
			Message: fmt.Sprintf("широта %f вне диапазона [-90; 90]", obs.Lat),
		})
	}
	if obs.Lon < -180 || obs.Lon > 180 {
		errs = append(errs, ValidationError{
			Field:   "station.observer.lon",
			Message: fmt.Sprintf("долгота %f вне диапазона [-180; 180]", obs.Lon),
		})
	}
	if obs.AltM < 0 || obs.AltM > 8000 {
		errs = append(errs, ValidationError{
			Field:   "station.observer.alt_m",
			Message: fmt.Sprintf("высота %f вне диапазона [0; 8000] м", obs.AltM),
		})
	}

	if c.TLE.UpdateInterval <= 0 {
		errs = append(errs, ValidationError{
			Field: "tle.update_interval", Message: "интервал должен быть положительным",
		})
	}

	if c.SatNOGS.CacheTTL <= 0 {
		errs = append(errs, ValidationError{
			Field: "satnogs.cache_ttl", Message: "TTL должен быть положительным",
		})
	}

	if strings.TrimSpace(c.ExcludeNoradFile) == "" {
		errs = append(errs, ValidationError{
			Field: "exclude_norad_file", Message: "путь не может быть пустым",
		})
	}

	if len(c.Station.RadioPaths) > 0 {
		seenIDs := make(map[int]int, len(c.Station.RadioPaths))
		for i, rp := range c.Station.RadioPaths {
			prefix := fmt.Sprintf("station.radio_paths[%d]", i)
			if rp.ID <= 0 {
				errs = append(errs, ValidationError{
					Field: prefix + ".id", Message: "id должен быть положительным",
				})
			} else if dup, exists := seenIDs[rp.ID]; exists {
				errs = append(errs, ValidationError{
					Field:   prefix + ".id",
					Message: fmt.Sprintf("дубликат id=%d (повторяет radio_paths[%d])", rp.ID, dup),
				})
			} else {
				seenIDs[rp.ID] = i
			}
			if strings.TrimSpace(rp.Name) == "" {
				errs = append(errs, ValidationError{
					Field: prefix + ".name", Message: "имя тракта не может быть пустым",
				})
			}
			lo, hi := rp.Antenna.FreqRangeMHz[0], rp.Antenna.FreqRangeMHz[1]
			if lo <= 0 || hi <= 0 || lo >= hi {
				errs = append(errs, ValidationError{
					Field:   prefix + ".antenna.freq_range_mhz",
					Message: fmt.Sprintf("неверный диапазон [%g; %g] МГц", lo, hi),
				})
			}
			if rp.Rotator != nil {
				if rp.Rotator.Port <= 0 || rp.Rotator.Port > 65535 {
					errs = append(errs, ValidationError{
						Field:   prefix + ".rotator.port",
						Message: fmt.Sprintf("порт %d вне диапазона [1; 65535]", rp.Rotator.Port),
					})
				}
			}
		}
	}

	if errs.HasErrors() {
		return errs
	}
	return nil
}

// RestartRequiredFields сравнивает старую и новую конфигурацию и возвращает
// список путей полей, для применения которых нужен перезапуск сервера.
//
// Hot-reload получают: ui.theme, station.observer.* (через ConfigStore.Subscribe).
// Restart нужен для server.port, tle.cache_dir/groups/update_interval/max_age,
// satnogs.enabled, station.radio_paths (инициализация SDR-устройств), DEV_MODE.
func RestartRequiredFields(old, new *Config) []string {
	if old == nil || new == nil {
		return nil
	}
	var fields []string

	if old.Server.Port != new.Server.Port {
		fields = append(fields, "server.port")
	}
	if old.TLE.CacheDir != new.TLE.CacheDir {
		fields = append(fields, "tle.cache_dir")
	}
	if !stringSlicesEqual(old.TLE.Groups, new.TLE.Groups) {
		fields = append(fields, "tle.groups")
	}
	if old.TLE.UpdateInterval != new.TLE.UpdateInterval {
		fields = append(fields, "tle.update_interval")
	}
	if old.TLE.MaxTLEAgeDays != new.TLE.MaxTLEAgeDays {
		fields = append(fields, "tle.max_tle_age_days")
	}
	if old.SatNOGS.Enabled != new.SatNOGS.Enabled {
		fields = append(fields, "satnogs.enabled")
	}
	if old.ExcludeNoradFile != new.ExcludeNoradFile {
		fields = append(fields, "exclude_norad_file")
	}
	if !radioPathsEqual(old.Station.RadioPaths, new.Station.RadioPaths) {
		fields = append(fields, "station.radio_paths")
	}
	return fields
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func radioPathsEqual(a, b []RadioPath) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Name != b[i].Name {
			return false
		}
		if a[i].Antenna != b[i].Antenna {
			return false
		}
		if a[i].Receiver != b[i].Receiver {
			return false
		}
		if !rotatorEqual(a[i].Rotator, b[i].Rotator) {
			return false
		}
	}
	return true
}

func rotatorEqual(a, b *RotatorConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
