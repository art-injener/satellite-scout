package dsp

import (
	"math"
	"math/cmplx"
	"testing"
	"time"
)

// --- DesignLowpass ---

func TestDesignLowpass_Size(t *testing.T) {
	taps := DesignLowpass(10000, 100000, 31, WindowHann)
	if len(taps) != 31 {
		t.Errorf("len=%d, want 31", len(taps))
	}
}

func TestDesignLowpass_EvenBecomesOdd(t *testing.T) {
	taps := DesignLowpass(10000, 100000, 30, WindowHann)
	if len(taps)%2 == 0 {
		t.Errorf("len=%d, should be odd (symmetric FIR)", len(taps))
	}
}

func TestDesignLowpass_Symmetric(t *testing.T) {
	taps := DesignLowpass(5000, 48000, 51, WindowBlackmanHarris)
	n := len(taps)
	for i := 0; i < n/2; i++ {
		if math.Abs(taps[i]-taps[n-1-i]) > 1e-12 {
			t.Errorf("not symmetric: taps[%d]=%g != taps[%d]=%g", i, taps[i], n-1-i, taps[n-1-i])
		}
	}
}

func TestDesignLowpass_DCGainOne(t *testing.T) {
	taps := DesignLowpass(12000, 48000, 63, WindowHann)
	sum := 0.0
	for _, v := range taps {
		sum += v
	}
	if math.Abs(sum-1.0) > 1e-10 {
		t.Errorf("DC gain=%g, want 1.0", sum)
	}
}

// --- XlatingFIR ---

func TestXlatingFIR_OutputLength(t *testing.T) {
	taps := DesignLowpass(5000, 48000, 31, WindowHann)
	x := NewXlatingFIR(0, 48000, 4, taps)

	in := make([]complex64, 1024)
	out := x.Process(in)

	if len(out) != 1024/4 {
		t.Errorf("len=%d, want %d", len(out), 1024/4)
	}
}

func TestXlatingFIR_ShiftToneToDC(t *testing.T) {
	const (
		fs      = 48000.0
		fTone   = 5000.0
		decim   = 4
		numTaps = 63
		n       = 4096
	)
	taps := DesignLowpass(fs/float64(2*decim), fs, numTaps, WindowBlackmanHarris)
	x := NewXlatingFIR(fTone, fs, decim, taps)

	in := makeToneC64(n, fTone, fs)
	out := x.Process(in)

	// После сдвига тон на DC — выходные отсчёты должны иметь примерно
	// одинаковую амплитуду (постоянный сигнал).
	// Пропускаем первые numTaps/decim отсчётов — переходный процесс FIR.
	skip := numTaps/decim + 1
	if skip >= len(out) {
		t.Fatal("output too short for transient skip")
	}

	var sumMag float64
	for _, s := range out[skip:] {
		sumMag += cmplx.Abs(complex128(s))
	}
	avgMag := sumMag / float64(len(out)-skip)

	// Амплитуда должна быть > 0 (сигнал прошёл)
	if avgMag < 0.3 {
		t.Errorf("signal lost after shift: avg magnitude=%g", avgMag)
	}

	// Разброс фазы/амплитуды должен быть мал (DC = постоянный вектор)
	for i, s := range out[skip:] {
		mag := cmplx.Abs(complex128(s))
		if math.Abs(mag-avgMag)/avgMag > 0.05 {
			t.Errorf("out[%d] magnitude=%g, avg=%g — too much variation for DC", skip+i, mag, avgMag)
			break
		}
	}
}

func TestXlatingFIR_RejectsOutOfBand(t *testing.T) {
	const (
		fs      = 48000.0
		fTune   = 5000.0
		fSignal = 15000.0 // далеко от полосы пропускания
		decim   = 4
		numTaps = 63
		n       = 4096
	)
	cutoff := fs / float64(2*decim)
	taps := DesignLowpass(cutoff, fs, numTaps, WindowBlackmanHarris)
	x := NewXlatingFIR(fTune, fs, decim, taps)

	in := makeToneC64(n, fSignal, fs)
	out := x.Process(in)

	// После сдвига fSignal оказывается на (fSignal - fTune) = 10 кГц,
	// а cutoff = 6 кГц — значит сигнал должен быть подавлен.
	skip := numTaps/decim + 1
	var maxMag float64
	for _, s := range out[skip:] {
		mag := cmplx.Abs(complex128(s))
		if mag > maxMag {
			maxMag = mag
		}
	}

	// Blackman-Harris 63 tap даёт >60 dB подавления
	if maxMag > 0.05 {
		t.Errorf("out-of-band signal not rejected: max magnitude=%g", maxMag)
	}
}

func TestXlatingFIR_Stateful(t *testing.T) {
	const (
		fs    = 48000.0
		fTone = 3000.0
		decim = 2
	)
	taps := DesignLowpass(fs/float64(2*decim), fs, 31, WindowHann)
	x := NewXlatingFIR(fTone, fs, decim, taps)

	// Обрабатываем два блока подряд — результат должен быть таким же,
	// как если бы обработали один объединённый блок.
	tone := makeToneC64(512, fTone, fs)

	block1 := x.Process(tone[:256])
	block2 := x.Process(tone[256:])

	x2 := NewXlatingFIR(fTone, fs, decim, taps)
	full := x2.Process(tone)

	combined := append(block1, block2...)
	if len(combined) != len(full) {
		t.Fatalf("combined len=%d, full len=%d", len(combined), len(full))
	}

	for i := range combined {
		diff := cmplx.Abs(complex128(combined[i]) - complex128(full[i]))
		if diff > 1e-5 {
			t.Errorf("[%d] combined=%v, full=%v, diff=%g", i, combined[i], full[i], diff)
			break
		}
	}
}

func TestXlatingFIR_SetShift(t *testing.T) {
	const (
		fs    = 48000.0
		decim = 4
	)
	taps := DesignLowpass(fs/float64(2*decim), fs, 31, WindowHann)
	x := NewXlatingFIR(1000, fs, decim, taps)

	in := makeToneC64(256, 2000, fs)
	out1 := x.Process(in)

	// Перестраиваемся на 2000 — теперь тон попадает в полосу
	x.SetShift(2000)
	in2 := makeToneC64(2048, 2000, fs)
	out2 := x.Process(in2)

	// Пропускаем переходный процесс, проверяем что сигнал есть
	skip := len(taps)/decim + 1
	_ = out1

	var sumMag float64
	for _, s := range out2[skip:] {
		sumMag += cmplx.Abs(complex128(s))
	}
	avgMag := sumMag / float64(len(out2)-skip)
	if avgMag < 0.3 {
		t.Errorf("after SetShift: avg magnitude=%g, want >0.3", avgMag)
	}
}

// Drifting tone like a Doppler-shifted downlink: start offset plus linear drift.
const (
	driftFs      = 48000.0
	driftDecim   = 4
	driftF0      = 3000.0 // Hz
	driftRate    = 3000.0 // Hz/s, exaggerated so the tone leaves the passband in 1 s
	driftCutoff  = 1000.0
	driftNumTaps = 127
)

func driftShift(t time.Duration) float64 {
	return driftF0 + driftRate*t.Seconds()
}

func newDriftFIR() *XlatingFIR {
	taps := DesignLowpass(driftCutoff, driftFs, driftNumTaps, WindowBlackmanHarris)
	return NewXlatingFIR(driftF0, driftFs, driftDecim, taps)
}

func TestXlatingFIR_ShiftFuncRemovesDopplerDrift(t *testing.T) {
	x := newDriftFIR()
	x.SetShiftFunc(driftShift)

	out := x.Process(makeDriftingToneC64(int(driftFs), driftShift, driftFs))

	skip := driftNumTaps/driftDecim + 1
	outRate := x.OutputRate()
	var worst float64
	for i := skip + 1; i < len(out); i++ {
		d := complex128(out[i]) * cmplx.Conj(complex128(out[i-1]))
		freq := cmplx.Phase(d) * outRate / (2 * math.Pi)
		worst = math.Max(worst, math.Abs(freq))
	}
	if worst > 1 {
		t.Errorf("residual frequency after Doppler removal = %.3f Hz, want < 1 Hz", worst)
	}

	if mag := cmplx.Abs(complex128(out[len(out)-1])); mag < 0.9 {
		t.Errorf("tone lost at end of drift: magnitude=%g, want ~1", mag)
	}
}

func TestXlatingFIR_ConstantShiftLosesDriftingTone(t *testing.T) {
	x := newDriftFIR()

	out := x.Process(makeDriftingToneC64(int(driftFs), driftShift, driftFs))

	// After 1 s the tone is 3 kHz away from the tuning point, cutoff is 1 kHz.
	tail := out[len(out)*9/10:]
	var maxMag float64
	for _, s := range tail {
		maxMag = math.Max(maxMag, cmplx.Abs(complex128(s)))
	}
	if maxMag > 0.05 {
		t.Errorf("drifting tone still passes constant shift: max magnitude=%g", maxMag)
	}
}

func TestXlatingFIR_ShiftFuncStateful(t *testing.T) {
	in := makeDriftingToneC64(8000, driftShift, driftFs)

	x := newDriftFIR()
	x.SetShiftFunc(driftShift)
	combined := append(x.Process(in[:1001]), x.Process(in[1001:])...)

	x2 := newDriftFIR()
	x2.SetShiftFunc(driftShift)
	full := x2.Process(in)

	if len(combined) != len(full) {
		t.Fatalf("combined len=%d, full len=%d", len(combined), len(full))
	}
	for i := range combined {
		if diff := cmplx.Abs(complex128(combined[i]) - complex128(full[i])); diff > 1e-5 {
			t.Fatalf("[%d] combined=%v, full=%v, diff=%g", i, combined[i], full[i], diff)
		}
	}
}

func TestXlatingFIR_SetShiftCancelsShiftFunc(t *testing.T) {
	const fTone = 2000.0
	x := newDriftFIR()
	x.SetShiftFunc(driftShift)
	x.SetShift(fTone)

	out := x.Process(makeToneC64(4096, fTone, driftFs))

	skip := driftNumTaps/driftDecim + 1
	for i, s := range out[skip:] {
		if mag := cmplx.Abs(complex128(s)); math.Abs(mag-1) > 0.05 {
			t.Fatalf("out[%d] magnitude=%g, want ~1: constant shift must replace the function", skip+i, mag)
		}
	}
}

func TestXlatingFIR_OutputRate(t *testing.T) {
	taps := DesignLowpass(5000, 48000, 31, WindowHann)
	x := NewXlatingFIR(0, 48000, 6, taps)
	if x.OutputRate() != 8000 {
		t.Errorf("OutputRate=%g, want 8000", x.OutputRate())
	}
}

// makeToneC64 генерирует комплексную экспоненту на частоте freqHz при sample rate fs.
func makeToneC64(n int, freqHz, fs float64) []complex64 {
	out := make([]complex64, n)
	for i := range out {
		phase := 2.0 * math.Pi * freqHz * float64(i) / fs
		out[i] = complex64(complex(math.Cos(phase), math.Sin(phase)))
	}
	return out
}

// makeDriftingToneC64 is a unit carrier whose frequency follows freqHz(t): the phase
// accumulates sample by sample, as on the transmitter side.
func makeDriftingToneC64(n int, freqHz func(time.Duration) float64, fs float64) []complex64 {
	out := make([]complex64, n)
	phase := 0.0
	for i := range out {
		out[i] = complex64(complex(math.Cos(phase), math.Sin(phase)))
		t := time.Duration(float64(i) / fs * float64(time.Second))
		phase = math.Mod(phase+2*math.Pi*freqHz(t)/fs, 2*math.Pi)
	}
	return out
}
