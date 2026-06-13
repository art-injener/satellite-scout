package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrDurationEmpty — пустая строка в JSON-поле duration.
var ErrDurationEmpty = errors.New("duration must not be empty")

// Duration — интервал времени в config.json в человекочитаемом виде ("6h", "12s").
// Синтаксис — time.ParseDuration (единицы: ns, us, µs, ms, s, m, h).
//
//nolint:recvcheck // UnmarshalJSON требует pointer receiver; остальные методы — value
type Duration time.Duration

// Duration возвращает значение как time.Duration для передачи в сервисы.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// UnmarshalJSON принимает только JSON-строку, например "6h" или "1h30m".
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"6h\": %w", err)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ErrDurationEmpty
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// MarshalJSON сериализует интервал в компактную строку ("6h", "24h", "12s").
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(formatConfigDuration(time.Duration(d)))
}

// formatConfigDuration форматирует длительность для config.json без лишних нулей.
func formatConfigDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	sec := int64(d.Round(time.Second) / time.Second)
	if sec < 60 {
		return strconv.FormatInt(sec, 10) + "s"
	}
	minutes := sec / 60
	remSec := sec % 60
	if minutes < 60 {
		if remSec == 0 {
			return strconv.FormatInt(minutes, 10) + "m"
		}
		return strconv.FormatInt(minutes, 10) + "m" + strconv.FormatInt(remSec, 10) + "s"
	}
	h := minutes / 60
	remMin := minutes % 60
	if remMin == 0 {
		return strconv.FormatInt(h, 10) + "h"
	}
	return strconv.FormatInt(h, 10) + "h" + strconv.FormatInt(remMin, 10) + "m"
}
