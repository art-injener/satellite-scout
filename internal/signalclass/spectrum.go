package signalclass

import (
	"math"
	"math/cmplx"
	"slices"

	"gonum.org/v1/gonum/dsp/fourier"

	"github.com/art-injener/satellite-scout/internal/dsp"
)

const (
	// psdSize — БПФ спектра участка: при 25–50 кГц шаг 25–50 Гц, CW уже 500 Гц различим.
	psdSize = 1024
	// passFraction — доля полосы участка внутри фильтра вырезки: дальше спад фильтра.
	passFraction = 2 * extractCutoff
	// bandMarkRatio — во сколько раз сглаженный спектр выше шума, чтобы бин считался сигналом (3 дБ).
	bandMarkRatio = 2
	// bandSmoothBins — сглаживание спектра перед разметкой полосы.
	bandSmoothBins = 5
	// bandSearchFraction — как далеко от середины ищется начало полосы, доля половины годной части.
	bandSearchFraction = 0.3
	// bandGapHz — разрыв, через который полоса продолжается: линии ЧМ с тоном, провал
	// между тонами FSK и неровности спектра широкой ЧМ.
	bandGapHz = 20_000
	// bandWidePassHz — при годной части не уже этого провал до десятой доли полосы
	// не режет широкую ЧМ на куски.
	bandWidePassHz = 100_000
	// bandWideGapFraction — доля годной части, допустимая как провал у широкой ЧМ.
	bandWideGapFraction = 0.1
	// bandNoiseGuard — бинов между полосой и шумом, по которому меряется уровень.
	bandNoiseGuard = 4
	// occupiedShare — доля мощности сигнала в занятой полосе.
	occupiedShare = 0.99
	// lineHalfBins — полуширина линии в спектре: главный лепесток окна Ханна и уход за 0,5 с.
	lineHalfBins = 2
)

// psd — спектр мощности методом Уэлча: окна Ханна с перекрытием в половину, сумма по бинам
// равна средней мощности, ноль частоты посередине.
type psd struct {
	p     []float64
	binHz float64
	segs  int // сколько окон усреднено
}

func welch(iq []complex64, rate float64) psd {
	n := psdSize
	for n > 64 && len(iq) < 4*n {
		n /= 2
	}
	if len(iq) < n {
		return psd{}
	}
	spec := dsp.NewSpectrum(n)
	win := dsp.MakeWindowCoeffs(n, dsp.WindowHann)
	var w2 float64
	for _, w := range win {
		w2 += w * w
	}
	acc := make([]float64, n)
	buf := make([]complex64, n)
	segs := 0
	for start := 0; start+n <= len(iq); start += n / 2 {
		copy(buf, iq[start:start+n])
		dsp.ApplyWindow(buf, win)
		for i, v := range spec.PowerSpectrum(buf) {
			acc[i] += v
		}
		segs++
	}
	scale := float64(n) / w2 / float64(segs)
	for i := range acc {
		acc[i] *= scale
	}
	return psd{p: acc, binHz: rate / float64(n), segs: segs}
}

func (s psd) freq(i int) float64 { return float64(i-len(s.p)/2) * s.binHz }

// band — полоса сигнала в спектре участка.
type band struct {
	noise        float64 // шум на бин
	lo, hi       int     // бины над шумом
	loHz, hiHz   float64 // края полосы [lo, hi], Гц
	occLo, occHi float64 // края доли occupiedShare мощности сигнала, Гц
	medianHz     float64 // половина мощности сигнала ниже этой частоты
	signal       float64 // мощность сигнала в полосе
	lineFrac     float64 // доля мощности сигнала в самой сильной линии
}

func (b *band) occupied() float64 { return b.occHi - b.occLo }

// findBand находит полосу сигнала у середины участка. Шум — сначала нижний квантиль годной
// части, затем медиана бинов вне найденной полосы. false — над шумом ничего нет.
func findBand(s psd) (band, bool) {
	n := len(s.p)
	if n == 0 {
		return band{}, false
	}
	half := int(passFraction * float64(n) / 2)
	passLo, passHi := n/2-half, n/2+half
	pass := s.p[passLo : passHi+1]
	// Нижний квантиль усреднённого шума смещён вниз на 0,52/√K от среднего (K окон).
	noise := quantile(pass, 0.3) / max(1-0.524/math.Sqrt(float64(s.segs)), 0.5)
	smooth := movingAvg(s.p, bandSmoothBins)

	gap := max(3, int(bandGapHz/s.binHz))
	if passHz := float64(passHi-passLo) * s.binHz; passHz >= bandWidePassHz {
		gap = max(gap, int(bandWideGapFraction*float64(passHi-passLo)))
	}
	var b band
	for range 2 {
		lo, hi, ok := markBand(smooth, noise, passLo, passHi, gap)
		if !ok {
			return band{}, false
		}
		b.lo, b.hi = lo, hi
		var outside []float64
		for i := passLo; i <= passHi; i++ {
			if i < lo-bandNoiseGuard || i > hi+bandNoiseGuard {
				outside = append(outside, s.p[i])
			}
		}
		if len(outside) >= 16 {
			noise = quantile(outside, 0.5)
		}
	}
	b.noise = noise
	b.loHz, b.hiHz = s.freq(b.lo)-s.binHz/2, s.freq(b.hi)+s.binHz/2
	b.measure(s)
	return b, b.signal > 0
}

// markBand — бины над шумом вокруг середины, с разрывами не шире gap бинов.
func markBand(smooth []float64, noise float64, passLo, passHi, gap int) (int, int, bool) {
	n := len(smooth)
	marked := func(i int) bool { return smooth[i] > bandMarkRatio*noise }
	reach := int(bandSearchFraction * float64(passHi-passLo) / 2)
	start := -1
	for d := 0; d <= reach && start < 0; d++ {
		switch {
		case marked(n/2 - d):
			start = n/2 - d
		case marked(n/2 + d):
			start = n/2 + d
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	lo, hi := start, start
	for i, miss := start-1, 0; i >= passLo && miss <= gap; i-- {
		if marked(i) {
			lo, miss = i, 0
		} else {
			miss++
		}
	}
	for i, miss := start+1, 0; i <= passHi && miss <= gap; i++ {
		if marked(i) {
			hi, miss = i, 0
		} else {
			miss++
		}
	}
	return lo, hi, true
}

// measure — мощность сигнала, занятая полоса, медианная частота и доля сильнейшей линии.
func (b *band) measure(s psd) {
	ex := make([]float64, b.hi-b.lo+1)
	var total float64
	peak := 0
	for i := range ex {
		v := s.p[b.lo+i] - b.noise
		b.signal += v
		ex[i] = max(v, 0)
		total += ex[i]
		if ex[i] > ex[peak] {
			peak = i
		}
	}
	if total <= 0 {
		b.signal = 0
		return
	}
	var cum, line float64
	first, last, median := -1, 0, 0
	for i, v := range ex {
		cum += v
		if first < 0 && cum >= (1-occupiedShare)/2*total {
			first = i
		}
		if cum <= (1+occupiedShare)/2*total {
			last = i + 1
		}
		if cum < total/2 {
			median = i + 1
		}
		if i >= peak-lineHalfBins && i <= peak+lineHalfBins {
			line += v
		}
	}
	last = min(last, len(ex)-1)
	b.occLo = s.freq(b.lo+first) - s.binHz/2
	b.occHi = s.freq(b.lo+last) + s.binHz/2
	b.medianHz = s.freq(b.lo + median)
	b.lineFrac = line / total
}

// clockLine — самая сильная спектральная линия ряда x в [fmin, fmax]: частота и превышение
// над медианой спектра вокруг неё. Так находятся такты по переходам.
func clockLine(x []float64, rate, fmin, fmax float64) (float64, float64) {
	n := 1
	for n < len(x) {
		n <<= 1
	}
	var mean float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(len(x))
	buf := make([]float64, n)
	for i, v := range x {
		buf[i] = v - mean
	}
	c := fourier.NewFFT(n).Coefficients(nil, buf)
	pow := make([]float64, len(c))
	for i, v := range c {
		pow[i] = real(v)*real(v) + imag(v)*imag(v)
	}
	kmin := max(1, int(math.Ceil(fmin*float64(n)/rate)))
	kmax := min(len(pow)-2, int(fmax*float64(n)/rate))
	if kmax <= kmin+2 {
		return 0, 0
	}
	k := kmin
	for i := kmin; i <= kmax; i++ {
		if pow[i] > pow[k] {
			k = i
		}
	}
	var around []float64
	for i := k * 3 / 4; i <= min(k*5/4, len(pow)-1); i++ {
		if i < k-4 || i > k+4 {
			around = append(around, pow[i])
		}
	}
	if len(around) < 8 {
		return 0, 0
	}
	bg := quantile(around, 0.5)
	if bg <= 0 {
		return 0, 0
	}
	// Вершина параболы по логарифмам трёх соседних бинов.
	a, b, cc := math.Log(pow[k-1]+1e-300), math.Log(pow[k]+1e-300), math.Log(pow[k+1]+1e-300)
	delta := 0.0
	if den := a - 2*b + cc; den < 0 {
		delta = 0.5 * (a - cc) / den
	}
	return (float64(k) + delta) * rate / float64(n), pow[k] / bg
}

func quantile(x []float64, q float64) float64 {
	s := slices.Clone(x)
	slices.Sort(s)
	return s[min(len(s)-1, int(q*float64(len(s))))]
}

// movingAvg — скользящее среднее по w отсчётам, с центром на отсчёте.
func movingAvg(x []float64, w int) []float64 {
	out := make([]float64, len(x))
	if w <= 1 {
		copy(out, x)
		return out
	}
	prefix := make([]float64, len(x)+1)
	for i, v := range x {
		prefix[i+1] = prefix[i] + v
	}
	for j := range out {
		lo, hi := max(0, j-w/2), min(len(x), j-w/2+w)
		out[j] = (prefix[hi] - prefix[lo]) / float64(hi-lo)
	}
	return out
}

// phaseStep — поворот фазы от b к a, радиан.
func phaseStep(a, b complex64) float64 {
	return cmplx.Phase(complex128(a) * cmplx.Conj(complex128(b)))
}
