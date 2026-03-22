package services

import (
	"testing"
	"time"
)

func TestFormatSessionTableColumns(t *testing.T) {
	aos := time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)
	los := aos.Add(5 * time.Minute)
	aosMs := aos.UnixMilli()
	losMs := los.UnixMilli()

	t.Run("before_AOS", func(t *testing.T) {
		now := aos.Add(-2 * time.Minute)
		dur, until := FormatSessionTableColumns(aosMs, losMs, now)
		if dur != "5м 00с" {
			t.Errorf("duration column: want %q, got %q", "5м 00с", dur)
		}
		if until != "2м 00с" {
			t.Errorf("until column: want %q, got %q", "2м 00с", until)
		}
	})

	t.Run("during_pass", func(t *testing.T) {
		now := aos.Add(2 * time.Minute)
		dur, until := FormatSessionTableColumns(aosMs, losMs, now)
		if until != "сейчас" {
			t.Errorf("until column: want %q, got %q", "сейчас", until)
		}
		if dur != "3м 00с" {
			t.Errorf("duration column (remaining): want %q, got %q", "3м 00с", dur)
		}
	})

	t.Run("after_LOS", func(t *testing.T) {
		now := los.Add(time.Minute)
		dur, until := FormatSessionTableColumns(aosMs, losMs, now)
		if dur != "—" || until != "—" {
			t.Errorf("after LOS: want em-dash for both, got %q / %q", dur, until)
		}
	})
}
