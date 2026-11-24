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

func TestDecideHold_ProbeLastsFiveSeconds(t *testing.T) {
	w := Window{CenterHz: 437_000_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_000_000, Mode: "FM", AOS: start - 10_000, LOS: start + 300_000}}
	// On the live waterfall a beacon burst needs about 5 s after retune to show up in the detector.
	if got := DecideHold(w, start, start, 60_000, plan, nil); got != start+5_000 {
		t.Errorf("got %d, want probe end %d", got, start+5_000)
	}
}

func TestDecideHold_DopplerShiftedSameNoradExtends(t *testing.T) {
	w := Window{CenterHz: 436_400_000, SpanHz: 2_048_000}
	const start int64 = 1_000_000
	plan := []Entry{{NoradID: 63218, FreqHz: 436_400_000, Mode: "BPSK", Baud: 9600, AOS: start - 1, LOS: start + 300_000}}
	// Bandscan already bound the segment to the satellite with Doppler +9.3 kHz (beyond NearHz).
	sights := []Sighting{{
		CenterHz: 436_409_300, NoradID: 63218, SNRDB: 14, Origin: "satellite", Emission: "pulsed",
	}}
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != start+TelemetryFloorMs {
		t.Fatalf("got %d, want telemetry hold %d", got, start+TelemetryFloorMs)
	}
}

func TestDecideHold_DopplerShiftedOtherNoradStillHoldsBand(t *testing.T) {
	w := Window{CenterHz: 436_400_000, SpanHz: 2_048_000}
	const start int64 = 1_000_000
	plan := []Entry{{NoradID: 63218, FreqHz: 436_400_000, Mode: "BPSK", Baud: 9600, AOS: start - 1, LOS: start + 300_000}}
	// Чужой NORAD не матчит план, но энергия в полосе — стоим пол для классификации.
	sights := []Sighting{{
		CenterHz: 436_409_300, NoradID: 11111, SNRDB: 14, Origin: "satellite", Emission: "pulsed",
	}}
	if got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights); got != start+TelemetryFloorMs {
		t.Fatalf("got %d, want band-energy hold %d", got, start+TelemetryFloorMs)
	}
}

func TestDecideHold_TelemetryExtendsByBaudFloor(t *testing.T) {
	w := Window{CenterHz: 437_000_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 437_000_000, Mode: "AFSK", Baud: 1200, AOS: start - 1, LOS: start + 300_000}}
	hold := telemetryDwellMs(1200)
	// LastSeen у края стоянки: wake ≥ цели, возвращаем полный hold.
	sights := []Sighting{{
		CenterHz: 437_000_000, SNRDB: 12, Origin: "satellite", Emission: "pulsed",
		LastSeenMs: start + hold - 1_000,
	}}
	got := DecideHold(w, start, start+hold-1_000, 60_000, plan, sights)
	if got != start+hold {
		t.Errorf("got %d, want telemetry hold %d", got, start+hold)
	}
}

func TestDecideHold_SSTVExtendsTo120s(t *testing.T) {
	w := Window{CenterHz: 437_550_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{Name: "ISS", FreqHz: 437_550_000, Mode: "SSTV", AOS: start - 1, LOS: start + 600_000}}
	sights := []Sighting{{CenterHz: 437_550_000, SNRDB: 15, LastSeenMs: start + ProbeMs}}
	// После пробы сразу честная цель SSTV, без промежуточных wake на +10 с.
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != start+SSTVHoldMs {
		t.Errorf("after probe got %d, want SSTV %d", got, start+SSTVHoldMs)
	}
	// Пока тишина короче GoneMs, цель не дёргается.
	now := start + 100_000
	sights[0].LastSeenMs = now - GoneMs + 1_000
	got = DecideHold(w, start, now, 60_000, plan, sights)
	if got != start+SSTVHoldMs {
		t.Errorf("mid-hold within GoneMs got %d, want SSTV %d", got, start+SSTVHoldMs)
	}
	// После GoneMs без сигнала — уход немедленно.
	sights[0].LastSeenMs = now - GoneMs - 1
	got = DecideHold(w, start, now, 60_000, plan, sights)
	if got != now {
		t.Errorf("after GoneMs got %d, want leave now %d", got, now)
	}
}

func TestDecideHold_FreshLastSeenDoesNotLoopTenSecondWake(t *testing.T) {
	w := Window{CenterHz: 437_000_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{
		NoradID: 25544, FreqHz: 437_000_000, Mode: "AFSK", Baud: 1200,
		AOS: start - 1, LOS: start + 300_000,
	}}
	// И при непрерывной энергии, и при импульсном маяке (пауза 5 с < GoneMs) цель стабильна.
	for _, tc := range []struct {
		now, lastSeen int64
	}{
		{start + ProbeMs, start + ProbeMs},
		{start + 10_000, start + 5_000},
		{start + 15_000, start + 15_000},
	} {
		sights := []Sighting{{
			CenterHz: 437_000_000, NoradID: 25544, SNRDB: 14,
			Origin: "satellite", LastSeenMs: tc.lastSeen,
		}}
		got := DecideHold(w, start, tc.now, 60_000, plan, sights)
		want := start + TelemetryFloorMs
		if got != want {
			t.Fatalf("at %d (lastSeen %d) got %d, want full floor %d", tc.now, tc.lastSeen, got, want)
		}
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

func TestDecideHold_BandEnergyHoldsTelemetryFloor(t *testing.T) {
	w := Window{CenterHz: 436_400_000, SpanHz: 2_048_000}
	const start int64 = 1_000_000
	plan := []Entry{{NoradID: 63218, FreqHz: 436_400_000, Mode: "BPSK", Baud: 9600, AOS: start - 1, LOS: start + 300_000}}
	// Energy in the window but not matched to the planned TX: still hold for classification.
	// Without LastSeen there is no early wake — full telemetry floor.
	sights := []Sighting{{
		CenterHz: 436_224_000, SNRDB: 12, Origin: "unknown",
	}}
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != start+TelemetryFloorMs {
		t.Fatalf("got %d, want band-energy hold %d", got, start+TelemetryFloorMs)
	}
	sights[0].LastSeenMs = start + TelemetryFloorMs
	got = DecideHold(w, start, start+TelemetryFloorMs, 60_000, plan, sights)
	if got != start+TelemetryFloorMs {
		t.Fatalf("at floor got %d, want leave", got)
	}
}

func TestDecideHold_BandEnergyGoneLeavesEarly(t *testing.T) {
	w := Window{CenterHz: 436_400_000, SpanHz: 2_048_000}
	const start int64 = 1_000_000
	plan := []Entry{{NoradID: 63218, FreqHz: 436_400_000, Mode: "BPSK", Baud: 9600, AOS: start - 1, LOS: start + 300_000}}
	now := start + 12_000
	sights := []Sighting{{CenterHz: 436_224_000, SNRDB: 12, LastSeenMs: now - GoneMs - 1}}
	if got := DecideHold(w, start, now, 60_000, plan, sights); got != now {
		t.Fatalf("got %d, want leave now after band energy gone", got)
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

func TestDecideHold_BeaconPeriodExtendsHold(t *testing.T) {
	w := Window{CenterHz: 401_000_000, SpanHz: 2_000_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 401_000_000, Mode: "FSK", Baud: 9600, AOS: start - 1, LOS: start + 300_000}}
	// Период 40 с × 1.2 = 48 с > TelemetryFloorMs (30 с). Без LastSeen — без раннего wake.
	sights := []Sighting{{
		CenterHz: 401_000_000, SNRDB: 15, Origin: "satellite", Emission: "pulsed",
		BeaconPeriodS: 40, Modulation: "2FSK", Baud: 2400,
	}}
	want := start + int64(40*1.2*1000)
	got := DecideHold(w, start, start+ProbeMs, 60_000, plan, sights)
	if got != want {
		t.Fatalf("got %d, want period hold %d", got, want)
	}
}

func TestDecideHold_CWAndVoice(t *testing.T) {
	w := Window{CenterHz: 435_000_000, SpanHz: 2_000_000}
	const start int64 = 1_000_000
	cw := []Entry{{FreqHz: 435_100_000, Mode: "CW", AOS: start - 1, LOS: start + 300_000}}
	sights := []Sighting{{CenterHz: 435_100_000, SNRDB: 11, LastSeenMs: start + CWHoldMs - 1_000}}
	if got := DecideHold(w, start, start+CWHoldMs-1_000, 60_000, cw, sights); got != start+CWHoldMs {
		t.Errorf("CW got %d, want %d", got, start+CWHoldMs)
	}
	fm := []Entry{{FreqHz: 435_100_000, Mode: "FM", AOS: start - 1, LOS: start + 300_000}}
	sights[0].LastSeenMs = start + VoiceHoldMs - 1_000
	if got := DecideHold(w, start, start+VoiceHoldMs-1_000, 60_000, fm, sights); got != start+VoiceHoldMs {
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
		{"AFSK", "", 1200, TelemetryFloorMs},
		{"GMSK", "", 4800, TelemetryFloorMs},
		{"BPSK", "", 1200, TelemetryFloorMs},
		{"FSK", "", 9600, TelemetryFloorMs},
		{"FSK", "", 50, DwellMaxMs}, // очень медленный: формула упирается в потолок
		{"FM", "SO-50", 0, VoiceHoldMs},
		{"", "", 0, VoiceHoldMs},
	}
	for _, tc := range cases {
		if got := kindHoldMs(tc.mode, tc.name, tc.baud); got != tc.want {
			t.Errorf("%s/%s baud=%v: got %d, want %d", tc.mode, tc.name, tc.baud, got, tc.want)
		}
	}
}

func TestTelemetryDwellMs_FormulaFloorAndCap(t *testing.T) {
	if got := telemetryDwellMs(1200); got != TelemetryFloorMs {
		t.Errorf("1200 baud: got %d, want floor %d", got, TelemetryFloorMs)
	}
	if got := telemetryDwellMs(0); got != TelemetryFloorMs {
		t.Errorf("unknown baud: got %d", got)
	}
	if got := telemetryDwellMs(50); got != DwellMaxMs {
		t.Errorf("50 baud: got %d, want cap %d", got, DwellMaxMs)
	}
}

func TestHoldForSighting_ShortensAmbiguousEnergy(t *testing.T) {
	sat := holdForSighting("AFSK", "", 1200, Sighting{Origin: "satellite", Emission: "pulsed"})
	if sat != TelemetryFloorMs {
		t.Fatalf("satellite: got %d, want %d", sat, TelemetryFloorMs)
	}
	pulse := holdForSighting("FSK", "", 9600, Sighting{Origin: "unknown", Emission: "pulsed"})
	if pulse != TelemetryFloorMs {
		t.Fatalf("pulsed: got %d, want %d", pulse, TelemetryFloorMs)
	}
	terr := holdForSighting("AFSK", "", 1200, Sighting{Origin: "terrestrial", Emission: "continuous"})
	if terr != TerrestrialHoldMs {
		t.Fatalf("terrestrial: got %d, want %d", terr, TerrestrialHoldMs)
	}
	unk := holdForSighting("FSK", "", 9600, Sighting{Origin: "", Emission: ""})
	if unk != TerrestrialHoldMs {
		t.Fatalf("empty origin: got %d, want %d", unk, TerrestrialHoldMs)
	}
}

func TestDecideHold_TerrestrialEnergyLeavesBy8s(t *testing.T) {
	w := Window{CenterHz: 401_700_000, SpanHz: 1_600_000}
	const start int64 = 1_000_000
	plan := []Entry{{FreqHz: 401_700_000, Mode: "FSK", Baud: 9600, AOS: start - 1, LOS: start + 300_000}}
	sights := []Sighting{{
		CenterHz: 401_700_000, SNRDB: 12, Origin: "terrestrial", Emission: "continuous",
		LastSeenMs: start + TerrestrialHoldMs - 1_000,
	}}
	got := DecideHold(w, start, start+TerrestrialHoldMs-1_000, 60_000, plan, sights)
	if got != start+TerrestrialHoldMs {
		t.Errorf("got %d, want leave at %d", got, start+TerrestrialHoldMs)
	}
}

func TestDecideHoldAir_ProbeThenLeaveWithoutEnergy(t *testing.T) {
	w := Window{CenterHz: 434_000_000, SpanHz: 2_000_000}
	const start int64 = 1_000_000
	if got := DecideHoldAir(w, start, start, nil); got != start+ProbeMs {
		t.Errorf("at start got %d, want probe %d", got, start+ProbeMs)
	}
	if got := DecideHoldAir(w, start, start+ProbeMs, nil); got != start+ProbeMs {
		t.Errorf("after probe got %d, want leave %d", got, start+ProbeMs)
	}
}

func TestDecideHoldAir_OOKPulsedHoldsTelemetryFloor(t *testing.T) {
	w := Window{CenterHz: 434_421_000, SpanHz: 2_000_000}
	const start int64 = 1_000_000
	// LastSeen=0: без раннего wake по GoneMs (иначе стоянка схлопнется до 10 с).
	sights := []Sighting{{
		CenterHz: 434_421_000, SNRDB: 12, Emission: "pulsed",
		Modulation: "ook", Baud: 2587,
	}}
	got := DecideHoldAir(w, start, start+ProbeMs, sights)
	if got != start+TelemetryFloorMs {
		t.Fatalf("got %d, want OOK floor %d", got, start+TelemetryFloorMs)
	}
}

func TestDecideHoldAir_PeriodExtends(t *testing.T) {
	w := Window{CenterHz: 433_500_000, SpanHz: 2_000_000}
	const start int64 = 1_000_000
	sights := []Sighting{{
		CenterHz: 433_500_000, SNRDB: 14, Emission: "pulsed",
		Modulation: "ook", Baud: 480, BeaconPeriodS: 20,
	}}
	want := start + 40_000 // 20 с × 2
	got := DecideHoldAir(w, start, start+ProbeMs, sights)
	if got != want {
		t.Fatalf("got %d, want period hold %d", got, want)
	}
}

func TestExtendHoldForRx_ActiveFramesAndPicturePreventCutoff(t *testing.T) {
	const start int64 = 1_000_000

	// Frame from a previous dwell must not extend the new window.
	if got := ExtendHoldForRx(start+ProbeMs, start, start+ProbeMs, start-1_000, false); got != start+ProbeMs {
		t.Fatalf("previous-dwell frame extended hold: got %d, want %d", got, start+ProbeMs)
	}

	// Recent valid frame near end of telemetry floor extends by RxGraceMs.
	now := start + TelemetryFloorMs
	lastFrame := now - 4_000
	want := lastFrame + RxGraceMs
	if got := ExtendHoldForRx(now, start, now, lastFrame, false); got != want {
		t.Fatalf("active frame got %d, want %d", got, want)
	}

	// After RxGraceMs of silence without new frames, no extension.
	silentNow := lastFrame + RxGraceMs
	if got := ExtendHoldForRx(silentNow, start, silentNow, lastFrame, false); got != silentNow {
		t.Fatalf("silent after grace got %d, want leave %d", got, silentNow)
	}

	// Frame burst is still capped at MaxHoldMs.
	late := start + MaxHoldMs - 2_000
	if got := ExtendHoldForRx(late, start, late, late, false); got != start+MaxHoldMs {
		t.Fatalf("frame cap got %d, want MaxHoldMs %d", got, start+MaxHoldMs)
	}

	// Active SSTV picture extends past 120 s up to RxPictureMaxHoldMs, then leaves once done.
	picNow := start + SSTVHoldMs
	if got := ExtendHoldForRx(picNow, start, picNow, 0, true); got != picNow+RxGraceMs {
		t.Fatalf("active picture got %d, want %d", got, picNow+RxGraceMs)
	}
	if got := ExtendHoldForRx(picNow, start, picNow, 0, false); got != picNow {
		t.Fatalf("finished picture got %d, want leave %d", got, picNow)
	}
	picLate := start + RxPictureMaxHoldMs - 1_000
	if got := ExtendHoldForRx(picLate, start, picLate, 0, true); got != start+RxPictureMaxHoldMs {
		t.Fatalf("picture cap got %d, want %d", got, start+RxPictureMaxHoldMs)
	}
}
