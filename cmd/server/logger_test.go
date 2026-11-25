package main

import (
	"log/slog"
	"testing"

	"github.com/art-injener/satellite-scout/internal/freqplan"
)

func TestParseScoutLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"info", slog.LevelInfo},
		{"DEBUG", slog.LevelDebug},
		{"debug", slog.LevelDebug},
		{"trace", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"nope", slog.LevelInfo},
		{"  Debug  ", slog.LevelDebug},
	}
	for _, tc := range cases {
		if got := parseScoutLogLevel(tc.in); got != tc.want {
			t.Errorf("parseScoutLogLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestShouldLogHoldAim(t *testing.T) {
	apply := freqplan.AimDecision{Apply: true, Reason: freqplan.AimReasonApplyEntry}
	if !shouldLogHoldAim(false, apply, "") {
		t.Fatal("apply must always log")
	}
	noSight := freqplan.AimDecision{Reason: freqplan.AimReasonNoSight}
	if !shouldLogHoldAim(true, noSight, "") {
		t.Fatal("new dwell no_sight must log")
	}
	if shouldLogHoldAim(false, noSight, freqplan.AimReasonNoSight) {
		t.Fatal("repeat no_sight must not spam")
	}
	if !shouldLogHoldAim(false, noSight, freqplan.AimReasonApplyEntry) {
		t.Fatal("reason change to no_sight must log")
	}
	done := freqplan.AimDecision{Reason: freqplan.AimReasonPhaseDone}
	if shouldLogHoldAim(false, done, freqplan.AimReasonPhaseDone) {
		t.Fatal("phase_done must stay quiet")
	}
}
