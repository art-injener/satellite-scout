package signalclass

import (
	"fmt"
	"math"
	"math/cmplx"

	"gonum.org/v1/gonum/dsp/fourier"

	"github.com/art-injener/satellite-scout/internal/modem"
)

const (
	// minWeakSNRDB — нижняя граница для когерентных и узкополосных веток ниже MinSNRDB.
	minWeakSNRDB = 6
	// pskMinRatio — превышение линии в z² или z⁴ над фоном спектра для PSK на нормальном SNR.
	pskMinRatio = 18
	// pskWeakMinRatio — более строгий порог линии в z²/z⁴ для слабых сигналов (6–9 дБ).
	pskWeakMinRatio = 25
	// pskMaxCarrierFrac — выше этой доли мощности в одной линии z сигнал считается несущей или AM.
	pskMaxCarrierFrac = 0.35
	// bpskMinLineFrac — минимальная доля мощности в линии спектра z².
	bpskMinLineFrac = 0.08
	// qpskMinLineFrac — минимальная доля мощности в линии спектра z⁴.
	qpskMinLineFrac = 0.05
	// bpskMinIQRatio — отношение дисперсий I и Q после снятия несущей и фазы BPSK.
	bpskMinIQRatio = 2.2
	// qpskMinC4 — минимальный модуль нормализованного 4-го фазового момента для QPSK.
	qpskMinC4 = 0.20
	// amMinCarrierRatio — превышение несущей над фоном спектра для AM.
	amMinCarrierRatio = 12
	// amMinCarrierFrac — минимальная доля мощности несущей в спектре AM.
	amMinCarrierFrac = 0.15
	// amMinModRMS — минимальный среднеквадратичный индекс амплитудной модуляции.
	amMinModRMS = 0.05
	// amMinSymmetry — минимальное отношение меньшей боковой полосы к большей.
	amMinSymmetry = 0.45
	// amMinIQRatio — преобладание синфазной (амплитудной) модуляции над квадратурной (фазовой).
	amMinIQRatio = 2.2
	// keyedMaxOffResidue — максимальная доля остаточной мощности над шумом в выключенном
	// состоянии для OOK/CW: отличает выключенный передатчик от минимумов огибающей AM.
	keyedMaxOffResidue = 0.04
)

// complexLine находит сильнейшую спектральную линию комплексного ряда:
// частоту (Гц), превышение пика над локальным фоном спектра и долю мощности в линии.
// Локальный фон (до ±64 бинов от пика) не проваливается в полосу задерживания ФНЧ.
func complexLine(x []complex64, rate float64) (float64, float64, float64) {
	s := welch(x, rate)
	n := len(s.p)
	if n < 32 {
		return 0, 0, 0
	}
	peak := 0
	var total float64
	for i, v := range s.p {
		total += v
		if v > s.p[peak] {
			peak = i
		}
	}
	if total <= 0 {
		return 0, 0, 0
	}
	var line float64
	for d := -lineHalfBins; d <= lineHalfBins; d++ {
		line += s.p[(peak+d+n)%n]
	}
	lineFrac := line / total

	halfWin := min(n/4, 64)
	around := make([]float64, 0, 2*halfWin)
	for d := -halfWin; d <= halfWin; d++ {
		if absInt(d) <= 6 {
			continue
		}
		around = append(around, s.p[(peak+d+n)%n])
	}
	if len(around) < 16 {
		return s.freq(peak), 0, lineFrac
	}
	bg := quantile(around, 0.5)
	if bg <= 0 {
		return s.freq(peak), 0, lineFrac
	}
	ratio := s.p[peak] / bg

	prev := s.p[(peak-1+n)%n]
	cur := s.p[peak]
	next := s.p[(peak+1)%n]
	a, b, c := math.Log(prev+1e-300), math.Log(cur+1e-300), math.Log(next+1e-300)
	delta := 0.0
	if den := a - 2*b + c; den < 0 {
		delta = 0.5 * (a - c) / den
	}
	freqHz := (float64(peak-n/2) + delta) * s.binHz
	return freqHz, ratio, lineFrac
}

// am распознаёт амплитудную модуляцию с несущей (DSB-AM): выраженная несущая,
// симметричные боковые полосы и преобладание синфазной модуляции над квадратурной.
func (a *analysis) am() (Result, bool) {
	bw := a.b.hiHz - a.b.loHz
	if len(a.on) < 256 || bw < 400 || a.occ > 25_000 {
		return Result{}, false
	}
	fc, r1, lf1 := complexLine(a.on, a.rate)
	if r1 < amMinCarrierRatio || lf1 < amMinCarrierFrac {
		return Result{}, false
	}

	// Снимаем несущую и удерживаем её фазу медленной ФАПЧ (полоса ~15 Гц),
	// чтобы ошибка оценки fc в 1–2 Гц не поворачивала I в Q за длительность отрезка.
	rot := shiftComplex(a.on, a.rate, -fc)
	bb := lockCarrierPhase(rot, a.rate)
	if len(bb) < 128 {
		return Result{}, false
	}
	// Пропускаем первые ~20 мс установления ФАПЧ.
	skip := min(len(bb)/4, int(0.02*a.rate))
	body := bb[skip:]

	var sumI, sumI2, sumQ, sumQ2 float64
	for _, v := range body {
		re, im := float64(real(v)), float64(imag(v))
		sumI += re
		sumI2 += re * re
		sumQ += im
		sumQ2 += im * im
	}
	nf := float64(len(body))
	meanI := sumI / nf
	meanQ := sumQ / nf
	varI := max(0, sumI2/nf-meanI*meanI)
	varQ := max(0, sumQ2/nf-meanQ*meanQ)
	nAxis := a.nz / 2
	exI := varI - nAxis
	if exI <= 1.5*nAxis || varI < amMinIQRatio*max(varQ, 1e-18) {
		return Result{}, false
	}
	cPwr := meanI * meanI
	modRMS := math.Sqrt(exI / max(cPwr, 1e-18))
	if modRMS < amMinModRMS || cPwr/(cPwr+varI+varQ) < 0.15 {
		return Result{}, false
	}

	// Проверка симметрии верхней и нижней боковых полос вокруг несущей.
	sym, okSym := sidebandSymmetry(body, a.rate, bw)
	if !okSym || sym < amMinSymmetry {
		return Result{}, false
	}

	p := Params{OccupiedHz: round10(a.occ)}
	q := min((sym-amMinSymmetry)/(1-amMinSymmetry), (varI/max(varQ, 1e-18)-amMinIQRatio)/amMinIQRatio)
	why := fmt.Sprintf("несущая с симметричными боковыми (%s %%), синфазная модуляция", num(sym*100, 0))
	return a.result(ModAM, q, p, why), true
}

// lockCarrierPhase удерживает медленный дрейф фазы несущей в нуле (полоса петли ~15 Гц),
// оставляя звуковую амплитудную модуляцию на оси I, а фазовую на оси Q.
func lockCarrierPhase(x []complex64, rate float64) []complex64 {
	if len(x) == 0 || rate <= 0 {
		return nil
	}
	out := make([]complex64, len(x))
	// Начальная фаза по первым 10 мс.
	initN := min(len(x), max(16, int(0.01*rate)))
	var sum complex128
	for i := range initN {
		sum += complex128(x[i])
	}
	phase := cmplx.Phase(sum)
	freq := 0.0
	// Коэффициенты петли 2-го порядка с полосой ~15 Гц.
	wn := 2 * math.Pi * 15 / rate
	alpha := 1.414 * wn
	beta := wn * wn
	maxFreq := 2 * math.Pi * 100 / rate

	for i, v := range x {
		sin, cos := math.Sincos(phase)
		w := complex128(v) * complex(cos, -sin)
		out[i] = complex64(w)
		err := math.Atan2(imag(w), real(w))
		freq = max(-maxFreq, min(maxFreq, freq+beta*err))
		phase += freq + alpha*err
	}
	return out
}

func sidebandSymmetry(bb []complex64, rate, occ float64) (float64, bool) {
	s := welch(bb, rate)
	n := len(s.p)
	if n < 64 {
		return 0, false
	}
	center := n / 2
	loOff := max(lineHalfBins+2, int(math.Ceil(150/s.binHz)))
	hiOff := min(center-2, int(math.Ceil(min(max(occ/2, 2000), 8000)/s.binHz)))
	if hiOff <= loOff+2 {
		return 0, false
	}
	var outBins []float64
	for i := range n {
		d := absInt(i - center)
		if d > hiOff+4 {
			outBins = append(outBins, s.p[i])
		}
	}
	noise := 0.0
	if len(outBins) >= 8 {
		noise = quantile(outBins, 0.5)
	}
	var pUp, pLo float64
	for d := loOff; d <= hiOff; d++ {
		pUp += max(0, s.p[center+d]-noise)
		pLo += max(0, s.p[center-d]-noise)
	}
	top := max(pUp, pLo)
	if top <= 0 {
		return 0, false
	}
	return min(pUp, pLo) / top, true
}

// psk распознаёт BPSK (узкая линия в z²) и QPSK (узкая линия в z⁴ при отсутствии в z²).
func (a *analysis) psk(minRatio float64) (Result, bool) {
	if len(a.on) < 256 || a.occ < 300 {
		return Result{}, false
	}
	_, _, lf1 := complexLine(a.on, a.rate)
	if lf1 >= pskMaxCarrierFrac {
		return Result{}, false
	}

	// Возводим в степень единичные векторы фазы, чтобы провалы и всплески огибающей
	// на переходах не усиливались в 4-й степени и не забивали спектральную линию.
	u2 := make([]complex64, len(a.on))
	u4 := make([]complex64, len(a.on))
	for i, v := range a.on {
		c := complex128(v)
		m := cmplx.Abs(c)
		if m <= 1e-12 {
			continue
		}
		u := c / complex(m, 0)
		c2 := u * u
		u2[i] = complex64(c2)
		u4[i] = complex64(c2 * c2)
	}

	f2, r2, lf2 := complexLine(u2, a.rate)
	f4, r4, lf4 := complexLine(u4, a.rate)

	if r2 >= minRatio && lf2 >= bpskMinLineFrac && lf2 > 2.0*lf1 {
		if res, ok := a.confirmBPSK(f2/2, r2); ok {
			return res, true
		}
	}
	if r4 >= minRatio && lf4 >= qpskMinLineFrac && lf4 > 2.0*lf2 && lf4 > 2.0*lf1 {
		if res, ok := a.confirmQPSK(f4/4, r4); ok {
			return res, true
		}
	}
	return Result{}, false
}

func (a *analysis) confirmBPSK(f0, ratio2 float64) (Result, bool) {
	bb := shiftComplex(a.on, a.rate, -f0)
	var sumSq complex128
	for _, v := range bb {
		c := complex128(v)
		sumSq += c * c
	}
	phi := 0.5 * cmplx.Phase(sumSq)
	rot := cmplx.Rect(1, -phi)

	re := make([]float64, len(bb))
	var sumI, sumI2, sumQ, sumQ2 float64
	pos := 0
	for i, v := range bb {
		w := complex128(v) * rot
		ri, qi := real(w), imag(w)
		re[i] = ri
		sumI += ri
		sumI2 += ri * ri
		sumQ += qi
		sumQ2 += qi * qi
		if ri >= 0 {
			pos++
		}
	}
	nf := float64(len(bb))
	varI := max(0, sumI2/nf-(sumI/nf)*(sumI/nf))
	varQ := max(0, sumQ2/nf-(sumQ/nf)*(sumQ/nf))
	if varI < bpskMinIQRatio*max(varQ, 1e-18) {
		return Result{}, false
	}
	posShare := float64(pos) / nf
	if posShare < 0.20 || posShare > 0.80 {
		return Result{}, false
	}

	baud := a.bpskBaud(re)
	p := Params{OccupiedHz: round10(a.occ), Baud: baud}
	q := clamp01((ratio2 - pskMinRatio) / (2 * pskMinRatio))
	why := fmt.Sprintf("линия в z² (×%s), фаза 0/π", num(ratio2, 0))
	if baud > 0 {
		why += ", такты " + num(baud, 0)
	}
	return a.result(ModBPSK, q, p, why), true
}

func (a *analysis) confirmQPSK(f0, ratio4 float64) (Result, bool) {
	bb := shiftComplex(a.on, a.rate, -f0)
	var sum2, sum4 complex128
	cnt := 0
	for _, v := range bb {
		c := complex128(v)
		m := cmplx.Abs(c)
		if m <= 1e-12 {
			continue
		}
		u := c / complex(m, 0)
		u2 := u * u
		sum2 += u2
		sum4 += u2 * u2
		cnt++
	}
	if cnt < 64 {
		return Result{}, false
	}
	c2 := cmplx.Abs(sum2) / float64(cnt)
	c4 := cmplx.Abs(sum4) / float64(cnt)
	if c4 < qpskMinC4 || c4 < 1.8*max(c2, 0.01) {
		return Result{}, false
	}

	baud := a.qpskBaud(bb)
	p := Params{OccupiedHz: round10(a.occ), Baud: baud}
	q := clamp01((ratio4 - pskMinRatio) / (2 * pskMinRatio))
	why := fmt.Sprintf("линия в z⁴ (×%s), 4 состояния фазы", num(ratio4, 0))
	if baud > 0 {
		why += ", такты " + num(baud, 0)
	}
	return a.result(ModQPSK, q, p, why), true
}

func (a *analysis) bpskBaud(re []float64) float64 {
	if len(re) < 32 {
		return 0
	}
	e := make([]float64, len(re)-1)
	for i := range e {
		dx := re[i+1] - re[i]
		e[i] = dx * dx
	}
	fmax := min(1.2*a.occ, 0.45*a.rate)
	baud, ok := fundamentalClock(e, a.rate, 50, fmax)
	if !ok {
		return 0
	}
	// Проверка демодулятором BPSK при кандидате скорости; если половина скорости даёт
	// заметно лучший захват тактов, выбираем её.
	best := baud
	bestJ := bpskJitter(a.on, a.rate, baud)
	if half := snapBaud(baud / 2); half >= 300 {
		if jHalf := bpskJitter(a.on, a.rate, half); jHalf < bestJ*0.7 && jHalf < 0.12 {
			best = half
		}
	}
	return best
}

func bpskJitter(iq []complex64, rate, baud float64) float64 {
	dem := modem.NewBPSKDemod(modem.BPSKConfig{SampleRate: rate, Baud: baud})
	dem.Demodulate(nil, iq)
	return dem.ClockJitter()
}

func (a *analysis) qpskBaud(bb []complex64) float64 {
	if len(bb) < 32 {
		return 0
	}
	e := make([]float64, len(bb)-1)
	for i := range e {
		d := bb[i+1] - bb[i]
		re, im := float64(real(d)), float64(imag(d))
		e[i] = re*re + im*im
	}
	fmax := min(1.2*a.occ, 0.45*a.rate)
	baud, ok := fundamentalClock(e, a.rate, 50, fmax)
	if !ok {
		return 0
	}
	return baud
}

// fundamentalClock находит основную частоту тактов, опускаясь с гармоник 2x…8x
// к наименьшей кратной частоте, на которой также есть выраженная линия тактов.
func fundamentalClock(e []float64, rate, fmin, fmax float64) (float64, bool) {
	pow, n := realPowerSpectrum(e)
	kmin := max(1, int(math.Ceil(fmin*float64(n)/rate)))
	kmax := min(len(pow)-2, int(fmax*float64(n)/rate))
	if kmax <= kmin+2 {
		return 0, false
	}

	k, ratio := clockPeakAt(pow, kmin, kmax)
	if ratio < clockMinRatio {
		return 0, false
	}

	// Спускаемся к основной частоте, если на точной субгармонике тоже есть сильная линия.
	peakPow := pow[k]
	for _, div := range []int{8, 6, 5, 4, 3, 2} {
		sub := int(math.Round(float64(k) / float64(div)))
		if sub < kmin {
			continue
		}
		ks, rs := clockPeakAt(pow, sub-2, sub+2)
		if rs >= clockMinRatio && pow[ks] >= 0.25*peakPow {
			k = ks
			break
		}
	}

	a, b, cc := math.Log(pow[k-1]+1e-300), math.Log(pow[k]+1e-300), math.Log(pow[k+1]+1e-300)
	delta := 0.0
	if den := a - 2*b + cc; den < 0 {
		delta = 0.5 * (a - cc) / den
	}
	f := (float64(k) + delta) * rate / float64(n)
	return snapBaud(f), true
}

func realPowerSpectrum(e []float64) ([]float64, int) {
	n := 1
	for n < len(e) {
		n <<= 1
	}
	var mean float64
	for _, v := range e {
		mean += v
	}
	mean /= float64(len(e))
	buf := make([]float64, n)
	for i, v := range e {
		buf[i] = v - mean
	}
	c := fourier.NewFFT(n).Coefficients(nil, buf)
	pow := make([]float64, len(c))
	for i, v := range c {
		pow[i] = real(v)*real(v) + imag(v)*imag(v)
	}
	return pow, n
}

func clockPeakAt(pow []float64, lo, hi int) (int, float64) {
	lo = max(1, lo)
	hi = min(len(pow)-2, hi)
	if hi < lo {
		return 0, 0
	}
	k := lo
	for i := lo + 1; i <= hi; i++ {
		if pow[i] > pow[k] {
			k = i
		}
	}
	var around []float64
	for i := max(1, k*3/4); i <= min(k*5/4, len(pow)-1); i++ {
		if i < k-4 || i > k+4 {
			around = append(around, pow[i])
		}
	}
	if len(around) < 8 {
		return k, 0
	}
	bg := quantile(around, 0.5)
	if bg <= 0 {
		return k, 0
	}
	return k, pow[k] / bg
}

func shiftComplex(x []complex64, rate, shiftHz float64) []complex64 {
	out := make([]complex64, len(x))
	w := 2 * math.Pi * shiftHz / rate
	for i, v := range x {
		sin, cos := math.Sincos(w * float64(i))
		out[i] = complex64(complex128(v) * complex(cos, sin))
	}
	return out
}

// weakCoherent — когерентные и узкополосные ветки ниже MinSNRDB (6–9 дБ):
// телеграф, чистая несущая и BPSK/QPSK с большим выигрышем накопления в БПФ.
func (a *analysis) weakCoherent() (Result, bool) {
	if r, keyed := a.keyed(); keyed && r.Modulation == ModCW {
		return r, true
	}
	if a.b.lineFrac > 0.75 {
		d := instFreq(a.on, a.rate)
		if sstvToneShare(d, a.rate) < sstvToneMinShare {
			if _, isAM := a.am(); !isAM {
				p := Params{OccupiedHz: round10(a.occ)}
				return a.result(ModCarrier, (a.b.lineFrac-carrierLine)/(1-carrierLine), p,
					"одна узкая линия в спектре"), true
			}
		}
	}
	if r, ok := a.psk(pskWeakMinRatio); ok {
		return r, true
	}
	return Result{}, false
}
