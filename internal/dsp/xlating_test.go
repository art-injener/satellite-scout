package dsp

import (
	"math"
	"math/cmplx"
	"testing"
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
