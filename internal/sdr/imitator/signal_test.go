package imitator

import (
	"errors"
	"math"
	"math/cmplx"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/modem"
)

const (
	testRate   = 48000.0
	testCenter = 435_000_000
)

func testCarrier(offsetHz int) Signal {
	return Signal{Kind: SignalCarrier, FrequencyHz: uint64(testCenter + offsetHz)}
}

func newTestSource(t *testing.T, sig Signal) *signalSource {
	t.Helper()
	s, err := newSignalSource(sig, testRate)
	if err != nil {
		t.Fatalf("newSignalSource: %v", err)
	}
	return s
}

// meanFreq — средняя мгновенная частота (Гц): для несущей и FM с AFSK равна сдвигу от центра.
func meanFreq(x []complex64) float64 {
	sum := 0.0
	for i := 1; i < len(x); i++ {
		sum += cmplx.Phase(complex128(x[i]) * cmplx.Conj(complex128(x[i-1])))
	}
	return sum / float64(len(x)-1) * testRate / (2 * math.Pi)
}

func meanPower(x []complex64) float64 {
	sum := 0.0
	for _, v := range x {
		re, im := float64(real(v)), float64(imag(v))
		sum += re*re + im*im
	}
	return sum / float64(len(x))
}

func isSilent(x []complex64) bool {
	for _, v := range x {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestSignal_CarrierAtOffsetFromCenter(t *testing.T) {
	s := newTestSource(t, testCarrier(10_000))
	x := make([]complex64, 4800)
	s.fill(x, testCenter)
	if f := meanFreq(x); math.Abs(f-10_000) > 1 {
		t.Errorf("mean freq=%.1f Hz, want 10000", f)
	}
}

func TestSignal_RetuneMovesSignal(t *testing.T) {
	s := newTestSource(t, testCarrier(10_000))
	x := make([]complex64, 4800)
	s.fill(x, testCenter)
	s.fill(x, testCenter+4_000)
	if f := meanFreq(x); math.Abs(f-6_000) > 1 {
		t.Errorf("mean freq after retune=%.1f Hz, want 6000", f)
	}
}

func TestSignal_OutsideBandIsSilent(t *testing.T) {
	s := newTestSource(t, testCarrier(30_000)) // дальше половины полосы 48 кГц
	x := make([]complex64, 4800)
	s.fill(x, testCenter)
	if !isSilent(x) {
		t.Error("signal outside receiver band must not appear")
	}
}

func TestSignal_LevelSetsAmplitude(t *testing.T) {
	sig := testCarrier(1_000)
	sig.LevelDBFS = -20
	s := newTestSource(t, sig)
	x := make([]complex64, 4800)
	s.fill(x, testCenter)
	for i, v := range x {
		if mag := cmplx.Abs(complex128(v)); math.Abs(mag-0.1) > 1e-5 {
			t.Fatalf("sample %d: |x|=%g, want 0.1 at -20 dBFS", i, mag)
		}
	}
}

func TestSignal_DopplerWithLinearDrift(t *testing.T) {
	sig := testCarrier(10_000)
	sig.DopplerHz = -3_000
	sig.DopplerRateHzS = 1_000
	s := newTestSource(t, sig)

	x := make([]complex64, 480) // 10 мс
	s.fill(x, testCenter)
	if f := meanFreq(x); math.Abs(f-7_005) > 2 {
		t.Errorf("at start mean freq=%.1f Hz, want 7005 (10k offset - 3k Doppler + drift)", f)
	}

	skip := make([]complex64, int(testRate)-len(x))
	s.fill(skip, testCenter)
	s.fill(x, testCenter)
	if f := meanFreq(x); math.Abs(f-8_005) > 2 {
		t.Errorf("after 1 s mean freq=%.1f Hz, want 8005 (+1 kHz drift)", f)
	}
}

// Ход туда-обратно: старт с центра вверх, +размах на четверти периода, −размах на трёх четвертях.
func TestSignal_SweepTriangle(t *testing.T) {
	sig := testCarrier(0)
	sig.SweepHz = 10_000
	sig.SweepPeriod = time.Second
	s := newTestSource(t, sig)

	window := make([]complex64, 48) // 1 мс: ход за окно 40 Гц
	at := func(sec float64) float64 {
		t.Helper()
		start := int(sec*testRate) - s.emitted
		if start > 0 {
			s.fill(make([]complex64, start), testCenter)
		}
		s.fill(window, testCenter)
		return meanFreq(window)
	}
	for _, c := range []struct{ sec, want float64 }{
		{0, 20}, {0.25, 9_980}, {0.5, -20}, {0.75, -9_980}, {1.25, 9_980},
	} {
		if f := at(c.sec); math.Abs(f-c.want) > 30 {
			t.Errorf("t=%.2f s: freq=%.0f Hz, want %.0f", c.sec, f, c.want)
		}
	}
}

// The sweep is a property of the air, not of the receiver: a narrower band does not shrink it.
func TestSignal_SweepNotLimitedByBand(t *testing.T) {
	sig := testCarrier(0)
	sig.SweepHz = 23_000 // above 45 % of 48 kHz, inside ±24 kHz
	sig.SweepPeriod = time.Second
	s := newTestSource(t, sig)

	x := make([]complex64, int(testRate)/4)
	s.fill(x, testCenter)
	peak := make([]complex64, 48)
	s.fill(peak, testCenter)
	if f := meanFreq(peak); math.Abs(f-23_000) > 50 {
		t.Errorf("sweep peak=%.0f Hz, want 23000", f)
	}
}

func TestSignal_SweepOutsideBandIsSilent(t *testing.T) {
	sig := testCarrier(0)
	sig.SweepHz = 1_000_000
	sig.SweepPeriod = time.Second
	s := newTestSource(t, sig)

	x := make([]complex64, int(testRate)/4)
	s.fill(x, testCenter)
	peak := make([]complex64, 48)
	s.fill(peak, testCenter)
	if !isSilent(peak) {
		t.Error("carrier swept 1 MHz away must be outside a 48 kHz band")
	}
}

// Профиль пролёта: пик в начале (середина пролёта), у горизонта на размах ниже.
func TestSignal_PassProfile(t *testing.T) {
	sig := testCarrier(1_000)
	sig.LevelDBFS = -20
	sig.PassSwingDB = 40
	sig.PassPeriod = time.Second
	s := newTestSource(t, sig)

	x := make([]complex64, int(testRate)+1)
	s.fill(x, testCenter)
	k := math.Pow(10, 4) - 1
	for _, c := range []struct{ sec, wantDB float64 }{
		{0, -20},
		{0.25, -20 - 10*math.Log10(1+k*0.25)},
		{0.5, -60},
		{0.75, -20 - 10*math.Log10(1+k*0.25)},
		{1, -20},
	} {
		v := x[int(c.sec*testRate)]
		if db := 20 * math.Log10(cmplx.Abs(complex128(v))); math.Abs(db-c.wantDB) > 0.01 {
			t.Errorf("t=%.2f s: level %.2f dBFS, want %.2f", c.sec, db, c.wantDB)
		}
	}
	for i, v := range x {
		if cmplx.Abs(complex128(v)) > 0.1+1e-6 {
			t.Fatalf("sample %d above the peak level", i)
		}
	}
}

func TestSignal_PassInvalidRejected(t *testing.T) {
	noPeriod := testCarrier(0)
	noPeriod.PassSwingDB = 10
	negative := testCarrier(0)
	negative.PassSwingDB = -10
	negative.PassPeriod = time.Second
	for name, sig := range map[string]Signal{"without period": noPeriod, "negative swing": negative} {
		if _, err := newSignalSource(sig, testRate); !errors.Is(err, ErrInvalidSignal) {
			t.Errorf("%s: err=%v, want ErrInvalidSignal", name, err)
		}
	}
}

func TestSignal_SweepWithoutPeriodRejected(t *testing.T) {
	sig := testCarrier(0)
	sig.SweepHz = 1_000
	if _, err := newSignalSource(sig, testRate); !errors.Is(err, ErrInvalidSignal) {
		t.Errorf("err=%v, want ErrInvalidSignal", err)
	}
}

// Манипуляция «есть/нет»: в потоке есть и несущая, и тишина.
func TestSignal_KeyedKindsHaveMarksAndSpaces(t *testing.T) {
	for _, kind := range []SignalKind{SignalOOK, SignalCW} {
		sig := testCarrier(0)
		sig.Kind = kind
		block := make([]complex64, testRate)
		newTestSource(t, sig).fill(block, testCenter)
		var marks, spaces int
		for _, v := range block {
			switch a := real(v)*real(v) + imag(v)*imag(v); {
			case a > 0.9:
				marks++
			case a < 0.01:
				spaces++
			}
		}
		if marks < len(block)/5 || spaces < len(block)/5 {
			t.Errorf("%s: %d marks and %d spaces in %d samples, want both", kind, marks, spaces, len(block))
		}
	}
}

func TestSignal_UnknownKindRejected(t *testing.T) {
	sig := testCarrier(0)
	sig.Kind = "noise"
	if _, err := newSignalSource(sig, testRate); !errors.Is(err, ErrInvalidSignal) {
		t.Errorf("err=%v, want ErrInvalidSignal", err)
	}
}

// Блоки разной длины дают тот же поток, что и один большой блок: фаза и время не рвутся.
func TestSignal_NoDiscontinuityBetweenBlocks(t *testing.T) {
	for _, kind := range []SignalKind{SignalCarrier, SignalAFSKPRBS, SignalOOK, SignalCW, SignalLFM} {
		sig := testCarrier(3_000)
		sig.Kind = kind
		sig.DopplerRateHzS = 500
		sig.SweepHz = 5_000
		sig.SweepPeriod = 200 * time.Millisecond
		sig.LevelDBFS = -10
		sig.PassSwingDB = 30
		sig.PassPeriod = 150 * time.Millisecond

		whole := make([]complex64, 9600)
		newTestSource(t, sig).fill(whole, testCenter)

		parts := newTestSource(t, sig)
		got := make([]complex64, 0, len(whole))
		for _, n := range []int{1, 777, 4096, 3000, 1726} {
			block := make([]complex64, n)
			parts.fill(block, testCenter)
			got = append(got, block...)
		}
		for i := range whole {
			if whole[i] != got[i] {
				t.Fatalf("%s: sample %d differs: %v vs %v", kind, i, got[i], whole[i])
			}
		}
	}
}

func TestSignal_AFSKPRBSModulatesSequence(t *testing.T) {
	sig := testCarrier(0)
	sig.Kind = SignalAFSKPRBS
	s := newTestSource(t, sig)
	x := make([]complex64, int(testRate))
	s.fill(x, testCenter)

	prbs := modem.NewPRBS9()
	levels := make([]byte, 1200)
	for i := range levels {
		levels[i] = prbs.Next()
	}
	want := modem.NewAFSK(modem.AFSK1200(testRate)).Modulate(nil, levels)
	for i := range x {
		if x[i] != want[i] {
			t.Fatalf("sample %d: %v, want %v (AFSK of PRBS-9)", i, x[i], want[i])
		}
	}
}

func TestSignal_AFSKPRBSContinuous(t *testing.T) {
	sig := testCarrier(5_000)
	sig.Kind = SignalAFSKPRBS
	s := newTestSource(t, sig)
	x := make([]complex64, int(testRate))
	s.fill(x, testCenter)
	for i, v := range x {
		if mag := cmplx.Abs(complex128(v)); math.Abs(mag-1) > 1e-4 {
			t.Fatalf("sample %d: |x|=%g, want 1 without pauses", i, mag)
		}
	}
	if f := meanFreq(x); math.Abs(f-5_000) > 50 {
		t.Errorf("mean freq=%.1f Hz, want 5000", f)
	}
}
