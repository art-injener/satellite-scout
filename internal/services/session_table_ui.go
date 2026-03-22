package services

import (
	"fmt"
	"time"
)

// FormatSessionTableColumns — значения для столбцов «длит.» и «до сеанса» в таблице плана пролетов.
// До AOS: длит. = полная длительность пролёта (LOS−AOS), до сеанса = обратный отсчёт до AOS.
// В окне [AOS, LOS]: до сеанса = «сейчас», длит. = оставшееся время до LOS.
// После LOS: «—» (строка не должна показываться в скользящем окне).
func FormatSessionTableColumns(aosMs, losMs int64, now time.Time) (colDuration, colUntil string) {
	if aosMs <= 0 || losMs <= 0 || losMs <= aosMs {
		return "—", "—"
	}
	aos := time.UnixMilli(aosMs).UTC()
	los := time.UnixMilli(losMs).UTC()
	total := los.Sub(aos)

	if now.Before(aos) {
		return formatRuDuration(total), formatRuCountdown(aos.Sub(now))
	}
	if !now.After(los) {
		return formatRuDuration(los.Sub(now)), "сейчас"
	}
	return "—", "—"
}

func formatRuDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	h := s / 3600
	m := (s % 3600) / 60
	sec := s % 60
	if h > 0 {
		return fmt.Sprintf("%dч %dм", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dм %02dс", m, sec)
	}
	return fmt.Sprintf("%dс", sec)
}

func formatRuCountdown(d time.Duration) string {
	if d <= 0 {
		return "0с"
	}
	return formatRuDuration(d)
}
