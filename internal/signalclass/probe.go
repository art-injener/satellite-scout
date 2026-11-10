package signalclass

import (
	"math"
	"math/cmplx"
	"slices"

	"gonum.org/v1/gonum/dsp/fourier"
)

const (
	// loraPeakRatio — пик БПФ дечирпованного символа к медиане бинов.
	loraPeakRatio = 8
	loraMinUp     = 4
	loraMinDown   = 2

	// fcchHz — тон коррекции частоты GSM, +67,7 кГц от несущей.
	fcchHz = 67_708.3
	// fcchGapS, fcchGapLongS — промежуток между тонами FCCH: 10 и 11 кадров.
	fcchGapS     = 0.04615
	fcchGapLongS = 0.05077
	fcchGapTolS  = 0.003
)

// ProbeLoRa — преамбула: несколько одинаковых восходящих свипов и нисходящие синхро.
// slopeHzS — наклон восходящего свипа. Один свип ответом не считается.
func ProbeLoRa(iq []complex64, rate, bandwidthHz, slopeHzS float64) bool {
	abs := math.Abs(slopeHzS)
	if rate <= 0 || bandwidthHz < 50_000 || abs < 1e6 || len(iq) == 0 {
		return false
	}
	nSym := int(math.Round(rate * bandwidthHz / abs))
	if nSym < 16 || len(iq) < nSym*6 {
		return false
	}
	up := abs
	if slopeHzS < 0 {
		up = -abs
	}
	return bestRun(iq, rate, nSym, up) >= loraMinUp && bestRun(iq, rate, nSym, -up) >= loraMinDown
}

// ProbeGSM — два тона FCCH с промежутком кадра BCCH. Один тон ответом не считается.
func ProbeGSM(iq []complex64, rate float64) bool {
	if rate < 80_000 || len(iq) < int(0.06*rate) {
		return false
	}
	win := max(8, int(0.00015*rate))
	hop := max(1, int(0.00005*rate))
	re := make([]float64, len(iq))
	im := make([]float64, len(iq))
	w := 2 * math.Pi * fcchHz / rate
	var phase float64
	for i, v := range iq {
		c, s := math.Cos(phase), math.Sin(phase)
		re[i] = float64(real(v))*c + float64(imag(v))*s
		im[i] = float64(imag(v))*c - float64(real(v))*s
		phase += w
	}
	var sr, si float64
	mags := make([]float64, 0, len(iq)/hop)
	for i := range re {
		sr += re[i]
		si += im[i]
		if i >= win {
			sr -= re[i-win]
			si -= im[i-win]
		}
		if i >= win && (i-win)%hop == 0 {
			mags = append(mags, math.Hypot(sr, si)/float64(win))
		}
	}
	if len(mags) < 8 {
		return false
	}
	med := medianFloat(mags)
	peak := slices.Max(mags)
	if peak < 1e-4 || peak < med*6 {
		return false
	}
	thr := math.Max(med*4, peak*0.3)
	dt := float64(hop) / rate
	var starts []float64
	in := false
	runStart := 0
	closeRun := func(end int) {
		dur := float64(end-runStart) * dt
		if dur >= 0.00025 && dur <= 0.0012 {
			starts = append(starts, float64(runStart)*dt)
		}
	}
	for i, m := range mags {
		if m >= thr {
			if !in {
				in = true
				runStart = i
			}
			continue
		}
		if in {
			in = false
			closeRun(i)
		}
	}
	if in {
		closeRun(len(mags))
	}
	for i := range starts {
		for j := i + 1; j < len(starts); j++ {
			gap := starts[j] - starts[i]
			if math.Abs(gap-fcchGapS) <= fcchGapTolS || math.Abs(gap-fcchGapLongS) <= fcchGapTolS {
				return true
			}
		}
	}
	return false
}

func bestRun(iq []complex64, rate float64, nSym int, removeSlope float64) int {
	nfft := 1
	for nfft < nSym {
		nfft <<= 1
	}
	fft := fourier.NewCmplxFFT(nfft)
	buf := make([]complex128, nfft)
	dst := make([]complex128, nfft)
	best := 0
	for _, off := range []int{0, nSym / 4, nSym / 2, 3 * nSym / 4} {
		if run := symbolRun(iq[off:], rate, nSym, removeSlope, fft, buf, dst); run > best {
			best = run
		}
	}
	return best
}

func symbolRun(iq []complex64, rate float64, nSym int, removeSlope float64, fft *fourier.CmplxFFT, buf, dst []complex128) int {
	best, run := 0, 0
	prev, have := 0, false
	for off := 0; off+nSym <= len(iq); off += nSym {
		bin, ratio := symbolPeak(iq[off:off+nSym], rate, removeSlope, fft, buf, dst)
		if ratio >= loraPeakRatio && (!have || absInt(bin-prev) <= 1) {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
		prev, have = bin, ratio >= loraPeakRatio
	}
	return best
}

func symbolPeak(sym []complex64, rate, removeSlope float64, fft *fourier.CmplxFFT, buf, dst []complex128) (int, float64) {
	for i := range buf {
		buf[i] = 0
	}
	for i, v := range sym {
		t := float64(i) / rate
		buf[i] = complex128(v) * cmplx.Rect(1, -math.Pi*removeSlope*t*t)
	}
	spec := fft.Coefficients(dst, buf)
	mags := make([]float64, len(spec))
	peak, bin := 0.0, 0
	for i, c := range spec {
		m := math.Hypot(real(c), imag(c))
		mags[i] = m
		if m > peak {
			peak, bin = m, i
		}
	}
	med := medianFloat(mags)
	if med <= 0 {
		return bin, 0
	}
	return bin, peak / med
}

func medianFloat(x []float64) float64 {
	s := slices.Clone(x)
	slices.Sort(s)
	return s[len(s)/2]
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func probeHit(id string, iq []complex64, rate float64, r Result) bool {
	switch id {
	case "lora":
		return ProbeLoRa(iq, rate, r.Params.OccupiedHz, r.Params.ChirpHzS)
	case "gsm_base":
		return ProbeGSM(iq, rate)
	default:
		return false
	}
}
