package freqplan

import "testing"

func TestDecideHold_ConfigOnlyUsesDwell(t *testing.T) {
	w := Window{CenterHz: 145_900_000, SpanHz: 1_600_000}
	const start, dwellMs int64 = 1_000_000, 60_000
	// Нет TX плана в окне — полный dwell конфига.
	if got := DecideHold(w, start, start, dwellMs, nil, nil); got != start+dwellMs {
		t.Errorf("at start got %d, want %d", got, start+dwellMs)
	}
	if got := DecideHold(w, start, start+dwellMs, dwellMs, nil, nil); got != start+dwellMs {
		// now == end: уход (возвращаем now, здесь равны)
		if got != start+dwellMs {
			t.Errorf("at end got %d, want leave at %d", got, start+dwellMs)
		}
	}
	if got := DecideHold(w, start, start+dwellMs+1, dwellMs, nil, nil); got != start+dwellMs+1 {
		t.Errorf("past end got %d, want now", got)
	}
}

func TestDecideHold_ProbeThenLeaveWithoutEnergy(t *testing.T) {
	w := Window{CenterHz: 437_000_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_000_000, Mode: "FM", AOS: start - 10_000, LOS: start + 300_000}}
	if got := DecideHold(w, start, start, 60_000, plan, nil); got != start+ProbeMs {
		t.Errorf("start got %d, want probe %d", got, start+ProbeMs)
	}
	if got := DecideHold(w, start, start+ProbeMs, 60_000, plan, nil); got != start+ProbeMs {
		t.Errorf("probe without energy got %d, want leave", got)
	}
}

func TestDecideHold_TelemetryExtendsTo20s(t *testing.T) {
	w := Window{CenterHz: 437_000_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_000_000, Mode: "AFSK", Baud: 1200, AOS: start - 1, LOS: start + 300_000}}
	sights := []Sighting{{CenterHz: 437_000_000, SNRDB: 12, LastSeenMs: start + ProbeMs}}
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != start+TelemetryHoldMs {
		t.Errorf("got %d, want telemetry hold %d", got, start+TelemetryHoldMs)
	}
}

func TestDecideHold_SSTVExtendsTo120s(t *testing.T) {
	w := Window{CenterHz: 437_550_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{Name: "ISS", FreqHz: 437_550_000, Mode: "SSTV", AOS: start - 1, LOS: start + 600_000}}
	sights := []Sighting{{CenterHz: 437_550_000, SNRDB: 15, LastSeenMs: start + ProbeMs}}
	// Сразу после пробы будим через GoneMs, чтобы ловить пропажу сигнала.
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != start+ProbeMs+GoneMs {
		t.Errorf("after probe got %d, want wake %d", got, start+ProbeMs+GoneMs)
	}
	// Пока LastSeen+GoneMs раньше цели — только короткий wake.
	sights[0].LastSeenMs = start + 100_000
	got = DecideHold(w, start, start+100_000, 60_000, plan, sights)
	if got != start+100_000+GoneMs {
		t.Errorf("mid-hold got %d, want wake %d", got, start+100_000+GoneMs)
	}
	// LastSeen у самого края вида — дотягиваем до 120 с.
	sights[0].LastSeenMs = start + SSTVHoldMs - 1_000
	got = DecideHold(w, start, start+SSTVHoldMs-1_000, 60_000, plan, sights)
	if got != start+SSTVHoldMs {
		t.Errorf("near end got %d, want SSTV %d", got, start+SSTVHoldMs)
	}
}

func TestDecideHold_LowSNRNoExtension(t *testing.T) {
	w := Window{CenterHz: 437_000_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_000_000, Mode: "AFSK", Baud: 1200, AOS: start - 1, LOS: start + 300_000}}
	sights := []Sighting{{CenterHz: 437_000_000, SNRDB: 9, LastSeenMs: start + ProbeMs}}
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != start+ProbeMs {
		t.Errorf("got %d, want leave at probe (SNR < 10)", got)
	}
}

func TestDecideHold_GoneAfterHoldMsLeavesEarly(t *testing.T) {
	w := Window{CenterHz: 437_550_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_550_000, Mode: "SSTV", AOS: start - 1, LOS: start + 600_000}}
	// После пробы продлили; сигнал пропал дольше GoneMs.
	now := start + 40_000
	sights := []Sighting{{CenterHz: 437_550_000, SNRDB: 15, LastSeenMs: now - GoneMs - 1}}
	got := DecideHold(w, start, now, 60_000, plan, sights)
	if got != now {
		t.Errorf("got %d, want leave now after signal gone", got)
	}
}

func TestDecideHold_Ceiling180s(t *testing.T) {
	w := Window{CenterHz: 437_550_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_550_000, Mode: "SSTV", AOS: start - 1, LOS: start + 900_000}}
	sights := []Sighting{{CenterHz: 437_550_000, SNRDB: 20, LastSeenMs: start + MaxHoldMs}}
	got := DecideHold(w, start, start+MaxHoldMs, 60_000, plan, sights)
	if got != start+MaxHoldMs {
		t.Errorf("got %d, want ceiling leave at %d", got, start+MaxHoldMs)
	}
}

func TestDecideHold_CWAndVoice(t *testing.T) {
	w := Window{CenterHz: 435_000_000, SpanHz: 2_000_000}
	const start int64 = 1_000_000
	cw := []Entry{{FreqHz: 435_100_000, Mode: "CW", AOS: start - 1, LOS: start + 300_000}}
	sights := []Sighting{{CenterHz: 435_100_000, SNRDB: 11, LastSeenMs: start + ProbeMs}}
	if got := DecideHold(w, start, start+ProbeMs, 60_000, cw, sights); got != start+CWHoldMs {
		t.Errorf("CW got %d, want %d", got, start+CWHoldMs)
	}
	fm := []Entry{{FreqHz: 435_100_000, Mode: "FM", AOS: start - 1, LOS: start + 300_000}}
	if got := DecideHold(w, start, start+ProbeMs, 60_000, fm, sights); got != start+VoiceHoldMs {
		t.Errorf("FM voice got %d, want %d", got, start+VoiceHoldMs)
	}
}

func TestKindHoldMs(t *testing.T) {
	cases := []struct {
		mode, name string
		baud       float64
		want       int64
	}{
		{"SSTV", "ISS", 0, SSTVHoldMs},
		{"FM", "SSTV Robot 36", 0, SSTVHoldMs},
		{"CW", "", 0, CWHoldMs},
		{"AFSK", "", 1200, TelemetryHoldMs},
		{"GMSK", "", 4800, TelemetryHoldMs},
		{"BPSK", "", 1200, TelemetryHoldMs},
		{"FSK", "", 9600, TelemetryHoldMs},
		{"FM", "SO-50", 0, VoiceHoldMs},
		{"", "", 0, VoiceHoldMs},
	}
	for _, tc := range cases {
		if got := kindHoldMs(tc.mode, tc.name, tc.baud); got != tc.want {
			t.Errorf("%s/%s baud=%v: got %d, want %d", tc.mode, tc.name, tc.baud, got, tc.want)
		}
	}
}
