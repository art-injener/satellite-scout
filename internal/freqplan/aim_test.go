package freqplan

import "testing"

func TestHoldAimOffsetHz_PeakMinusLOMinusDoppler(t *testing.T) {
	// LO 437.0, peak 437.008, Doppler +5 kHz → residual Offset +3 kHz.
	got := HoldAimOffsetHz(437_000_000, 437_008_000, 5_000)
	if got != 3_000 {
		t.Fatalf("got %d, want 3000", got)
	}
	if got := HoldAimOffsetHz(434_000_000, 434_000_000, 0); got != 0 {
		t.Fatalf("on-peak got %d, want 0", got)
	}
	if got := HoldAimOffsetHz(434_000_000, 433_992_000, 0); got != -8_000 {
		t.Fatalf("below LO got %d, want -8000", got)
	}
}

func TestHoldAimSight_WithinAimMatchAndWeakSNR(t *testing.T) {
	const lo uint64 = 435_000_000
	const now int64 = 1_000_000
	near := Sighting{CenterHz: 435_040_000, SNRDB: 6.5, LastSeenMs: now} // 40 кГц, слабый импульс
	far := Sighting{CenterHz: 435_200_000, SNRDB: 20, LastSeenMs: now}
	s, ok := HoldAimSight(lo, []Sighting{far, near}, now)
	if !ok || s.CenterHz != near.CenterHz {
		t.Fatalf("got %+v, want near weak pulse", s)
	}
	if _, ok := HoldAimSight(lo, []Sighting{far}, now); ok {
		t.Fatal("peak beyond AimMatchHz must not aim")
	}
}

func TestHoldAimSight_PrefersClosestOverLouderInWindow(t *testing.T) {
	const lo uint64 = 436_400_000
	const now int64 = 1_000_000
	// Громкая помеха в AimMatchHz не должна перетягивать с пика у LO.
	loud := Sighting{CenterHz: 436_317_000, SNRDB: 16, LastSeenMs: now}
	onLO := Sighting{CenterHz: 436_399_070, SNRDB: 7, LastSeenMs: now}
	s, ok := HoldAimSight(lo, []Sighting{loud, onLO}, now)
	if !ok || s.CenterHz != onLO.CenterHz {
		t.Fatalf("got %+v, want on-LO pulse", s)
	}
}

func TestDecideHoldAim_EntryFromRememberedThenLiveCorrection(t *testing.T) {
	const lo uint64 = 437_000_000
	const start, now int64 = 1_000_000, 1_000_000
	st := ResetAimState(start, lo)

	// Вход: память на +8 кГц от LO, Доплера нет.
	remembered := []Sighting{{CenterHz: 437_008_000, SNRDB: 14, LastSeenMs: now}}
	dec, st := DecideHoldAim(st, lo, remembered, nil, now, 0)
	if !dec.Apply || dec.OffsetHz != 8_000 {
		t.Fatalf("entry %+v, want apply offset 8000", dec)
	}
	if st.Phase != AimEntry || st.AimedAtHz != 437_008_000 {
		t.Fatalf("state after entry %+v", st)
	}

	// Тот же вызов снова: уже AimEntry, live пуст — без Apply.
	dec, st = DecideHoldAim(st, lo, remembered, nil, now+1_000, 0)
	if dec.Apply {
		t.Fatalf("second call must not re-aim every tick: %+v", dec)
	}

	// Live ушёл сразу в пробе — правим без ожидания конца пробы (короткий импульс).
	live := []Sighting{{CenterHz: 437_012_000, SNRDB: 15, LastSeenMs: now + 1_000}}
	dec, st = DecideHoldAim(st, lo, remembered, live, now+1_000, 0)
	if !dec.Apply || dec.OffsetHz != 12_000 {
		t.Fatalf("live %+v, want apply offset 12000", dec)
	}
	if st.Phase != AimLive {
		t.Fatalf("phase %v, want AimLive", st.Phase)
	}

	// Дальше не трогаем.
	dec, st = DecideHoldAim(st, lo, nil, live, now+2_000, 0)
	if dec.Apply || st.Phase != AimLive {
		t.Fatalf("after live must stay quiet: dec=%+v st=%+v", dec, st)
	}
}

func TestDecideHoldAim_SubtractsDopplerResidual(t *testing.T) {
	const lo uint64 = 436_400_000
	const now int64 = 1_000_000
	st := ResetAimState(now, lo)
	// Peak = LO + Doppler(+9.3 kHz) + catalog residual(+2 kHz).
	sight := []Sighting{{CenterHz: 436_411_300, SNRDB: 14, LastSeenMs: now}}
	dec, _ := DecideHoldAim(st, lo, sight, nil, now, 9_300)
	if !dec.Apply || dec.OffsetHz != 2_000 {
		t.Fatalf("got %+v, want residual offset 2000", dec)
	}
}

func TestDecideHoldAim_OperatorLockStopsAim(t *testing.T) {
	const lo uint64 = 434_000_000
	const now int64 = 1_000_000
	st := ResetAimState(now, lo)
	st.OperatorLocked = true
	sights := []Sighting{{CenterHz: 434_010_000, SNRDB: 16, LastSeenMs: now}}
	dec, st := DecideHoldAim(st, lo, sights, nil, now, 0)
	if dec.Apply {
		t.Fatalf("locked must not aim: %+v", dec)
	}
	if !st.OperatorLocked {
		t.Fatal("lock must stick")
	}
}

func TestDecideHoldAim_SkipsTinyDelta(t *testing.T) {
	const lo uint64 = 434_000_000
	const now int64 = 1_000_000
	st := ResetAimState(now, lo)
	sights := []Sighting{{CenterHz: 434_000_100, SNRDB: 12, LastSeenMs: now}}
	dec, st := DecideHoldAim(st, lo, sights, nil, now, 0)
	if dec.Apply {
		t.Fatalf("tiny delta must not apply: %+v", dec)
	}
	if st.Phase != AimEntry {
		t.Fatalf("phase %v, want AimEntry after tiny entry", st.Phase)
	}
}

func TestDecideHoldAim_LiveWithinDeltaClosesWithoutApply(t *testing.T) {
	const lo uint64 = 434_000_000
	const start int64 = 1_000_000
	st := ResetAimState(start, lo)
	st.Phase = AimEntry
	st.AimedAtHz = 434_008_000
	live := []Sighting{{CenterHz: 434_008_200, SNRDB: 13, LastSeenMs: start + ProbeMs}}
	dec, st := DecideHoldAim(st, lo, nil, live, start+ProbeMs, 0)
	if dec.Apply {
		t.Fatalf("stable live must not apply: %+v", dec)
	}
	if st.Phase != AimLive {
		t.Fatalf("phase %v, want AimLive", st.Phase)
	}
}
