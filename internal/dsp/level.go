package dsp

import (
	"math"
	"slices"
)

// peakWindow — длина окна пикового детектора порога по умолчанию, отсчётов.
// Около 1 мс при 48 кГц канала: короткий импульс виден, а одиночный гребень
// шума почти не поднимает мощность окна над средним.
const peakWindow = 48

// Параметры адаптивного порога шумоподавления.
const (
	AdaptiveSquelchMarginDB     = 6.0    // запас адаптивного порога над шумом канала, дБ
	AdaptiveSquelchHysteresisDB = 2.0    // гистерезис удержания открытого порога, дБ
	AdaptiveNoiseFloorDBFS      = -126.0 // нижний предел оценки шума (тишина в тестах)
)

// LevelDBFS возвращает среднюю мощность отсчётов в dBFS: тон единичной амплитуды — 0 dBFS.
// После фильтра канала это мощность в его полосе — сумма по полосе, а не уровень бина спектра,
// поэтому поправка на окно БПФ не нужна. Тишина и пустой блок — DBFloor.
func LevelDBFS(iq []complex64) float32 {
	if len(iq) == 0 {
		return DBFloor
	}
	var sum float64
	for _, s := range iq {
		re, im := float64(real(s)), float64(imag(s))
		sum += re*re + im*im
	}
	p := sum / float64(len(iq))
	if p <= 0 {
		return DBFloor
	}
	return max(float32(10*math.Log10(p)), DBFloor)
}

// PeakDBFS — наибольшая мощность короткого окна в блоке, dBFS.
// Для импульса короче блока средняя мощность почти не растёт, а окно показывает всплеск.
// Одиночный отсчёт шума окно почти не поднимает (в отличие от max |s|²).
func PeakDBFS(iq []complex64) float32 {
	return PeakWindowDBFS(iq, peakWindow)
}

// PeakWindowDBFS — max средней мощности по скользящему окну window отсчётов.
func PeakWindowDBFS(iq []complex64, window int) float32 {
	n := len(iq)
	if n == 0 {
		return DBFloor
	}
	if window < 1 {
		window = 1
	}
	if window > n {
		window = n
	}
	var sum float64
	for i := 0; i < window; i++ {
		re, im := float64(real(iq[i])), float64(imag(iq[i]))
		sum += re*re + im*im
	}
	best := sum
	for i := window; i < n; i++ {
		re, im := float64(real(iq[i])), float64(imag(iq[i]))
		sum += re*re + im*im
		re0, im0 := float64(real(iq[i-window])), float64(imag(iq[i-window]))
		sum -= re0*re0 + im0*im0
		if sum > best {
			best = sum
		}
	}
	p := best / float64(window)
	if p <= 0 {
		return DBFloor
	}
	return max(float32(10*math.Log10(p)), DBFloor)
}

// Squelch — порог демодулятора: блок канала, тише ThresholdDBFS, демодулятор не принимает.
// Порог держится в dBFS, как в настройках приёмника. HoldSamples держит порог открытым
// после последнего громкого блока: иначе блок, где сигнал кончается в начале, оказывается
// тише порога, и хвост кадра теряется. HysteresisDB опускает порог удержания для уже
// открытого канала, исключая дребезг на границе.
//
// Открытие — по средней мощности или по короткому окну огибающей: импульс маяка/брелока
// часто короче блока, и одна средняя мощность его не видит.
type Squelch struct {
	ThresholdDBFS float64
	HysteresisDB  float64
	HoldSamples   int
	PeakWindow    int // 0 — использовать peakWindow по умолчанию
	level         float32
	peak          float32
	holdLeft      int
	open          bool
	above         bool
}

// Open измеряет уровень и пик блока канала и сообщает, открыт ли порог для этого блока.
func (s *Squelch) Open(iq []complex64) bool {
	s.level = LevelDBFS(iq)
	win := s.PeakWindow
	if win <= 0 {
		win = peakWindow
	}
	s.peak = PeakWindowDBFS(iq, win)
	th := s.ThresholdDBFS
	if s.open && s.HysteresisDB > 0 {
		th -= s.HysteresisDB
	}
	s.above = float64(s.level) >= th || float64(s.peak) >= th
	if s.above {
		s.open = true
		s.holdLeft = s.HoldSamples
		return true
	}
	if s.holdLeft > 0 {
		s.holdLeft -= len(iq)
		s.open = true
		return true
	}
	s.open = false
	return false
}

// AboveThreshold — этот блок громче порога (с учётом гистерезиса, но без удержания).
func (s *Squelch) AboveThreshold() bool {
	return s.above
}

// LevelDBFS — средняя мощность последнего блока, переданного в Open.
func (s *Squelch) LevelDBFS() float32 {
	return s.level
}

// PeakDBFS — пик короткого окна последнего блока, переданного в Open.
func (s *Squelch) PeakDBFS() float32 {
	return s.peak
}

// SquelchPeakWindow подбирает длину окна огибающей для частоты отсчётов rate и полосы
// канала passbandHz: ~2 мс для широких каналов и не короче 4/passbandHz для узких,
// чтобы гребни гауссовского шума не открывали порог.
func SquelchPeakWindow(rate, passbandHz float64) int {
	if rate <= 0 {
		return peakWindow
	}
	dur := 0.002 // 2 мс для широких каналов
	if passbandHz > 0 {
		minDur := 4.0 / passbandHz
		if minDur > dur {
			dur = minDur
		}
	}
	if dur > 0.020 {
		dur = 0.020
	}
	w := max(int(math.Round(dur*rate)), 16)
	return w
}

// AdaptiveNoise оценивает мощность шума в полосе узкого канала по спектру широкого
// выхода грубого фильтра (обычно 64 кГц). Нижний перцентиль (30%) по плоской полосе
// грубого фильтра устойчив к непрерывным сигналам внутри канала и быстро отслеживает
// скачки усиления (АРУ). Не потокобезопасен: работает в горутине потока.
type AdaptiveNoise struct {
	spec     *Spectrum
	window   []float64
	winPower float64
	buf      []complex64
	avg      []float64
	sorted   []float64
	size     int

	hasEst  bool
	estDBFS float64
}

// Reset сбрасывает сглаженную оценку шума (при смене частоты, полосы или дискретизации).
func (a *AdaptiveNoise) Reset() {
	a.hasEst = false
	a.estDBFS = 0
}

func (a *AdaptiveNoise) ensure(n int) bool {
	size := 256
	for size > n && size > 64 {
		size >>= 1
	}
	if n < size {
		return false
	}
	if a.size == size && a.spec != nil {
		return true
	}
	a.size = size
	a.spec = NewSpectrum(size)
	a.window = MakeWindowCoeffs(size, WindowBlackmanHarris)
	var w2 float64
	for _, w := range a.window {
		w2 += w * w
	}
	a.winPower = w2 / float64(size)
	a.buf = make([]complex64, size)
	a.avg = make([]float64, size)
	a.sorted = make([]float64, 0, size)
	return true
}

// EstimateChannelNoiseDBFS возвращает сглаженную оценку шума (dBFS) в полосе passbandHz
// на основе широкого блока coarse (с частотой отсчётов midRate).
func (a *AdaptiveNoise) EstimateChannelNoiseDBFS(coarse []complex64, midRate, passbandHz float64) float64 {
	if midRate <= 0 || passbandHz <= 0 || !a.ensure(len(coarse)) {
		if a.hasEst {
			return a.estDBFS
		}
		return AdaptiveNoiseFloorDBFS
	}
	n := a.size
	segs := min(len(coarse)/n, 16)
	clear(a.avg)
	for s := range segs {
		copy(a.buf, coarse[s*n:(s+1)*n])
		ApplyWindow(a.buf, a.window)
		bins := a.spec.PowerSpectrum(a.buf)
		for i, p := range bins {
			a.avg[i] += p
		}
	}
	invSegs := 1.0 / float64(segs)
	for i := range a.avg {
		a.avg[i] *= invSegs
	}

	// Берём только плоскую полосу грубого фильтра [-0.35*midRate, +0.35*midRate],
	// чтобы скаты грубого ФНЧ не занижали оценку шумового пола.
	half := int(math.Floor(0.35 * float64(n)))
	center := n / 2
	lo := max(0, center-half)
	hi := min(n, center+half)
	if hi <= lo {
		return AdaptiveNoiseFloorDBFS
	}
	a.sorted = a.sorted[:0]
	for i := lo; i < hi; i++ {
		a.sorted = append(a.sorted, a.avg[i])
	}
	slices.Sort(a.sorted)

	// 30-й перцентиль остаётся в шуме, даже если канал и соседний сигнал занимают больше половины полосы.
	p30 := a.sorted[len(a.sorted)*3/10]
	rawDB := AdaptiveNoiseFloorDBFS
	if p30 > 0 && a.winPower > 0 {
		binNoise := p30 * gammaP30Correction(segs) / a.winPower
		chNoise := binNoise * float64(n) * (passbandHz / midRate)
		if chNoise > 0 {
			rawDB = 10 * math.Log10(chNoise)
		}
	}
	if rawDB < AdaptiveNoiseFloorDBFS {
		rawDB = AdaptiveNoiseFloorDBFS
	}

	if !a.hasEst {
		a.estDBFS = rawDB
		a.hasEst = true
		return a.estDBFS
	}
	alpha := 0.25
	if math.Abs(rawDB-a.estDBFS) > 6.0 {
		// Быстрая подстройка при скачке усиления (АРУ) или переключении диапазона.
		alpha = 0.5
	}
	a.estDBFS += alpha * (rawDB - a.estDBFS)
	return a.estDBFS
}

// gammaP30Correction возвращает поправку от 30-го перцентиля к среднему
// для распределения Gamma(k, 1/k) — суммы k экспоненциальных бинов белого шума.
func gammaP30Correction(k int) float64 {
	switch k {
	case 1:
		return 2.8037
	case 2:
		return 1.8226
	case 3:
		return 1.5679
	case 4:
		return 1.4353
	case 5:
		return 1.3631
	case 6:
		return 1.3182
	case 7:
		return 1.2855
	case 8:
		return 1.2609
	default:
		// Приближение Уилсона-Хилферти для 30-го перцентиля (z = -0.5244).
		kf := float64(max(1, k))
		t := 1.0 - 1.0/(9.0*kf) - 0.5244/(3.0*math.Sqrt(kf))
		q := t * t * t
		if q <= 0.1 {
			return 1.0
		}
		return 1.0 / q
	}
}
