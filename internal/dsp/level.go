package dsp

import "math"

// peakWindow — длина окна пикового детектора порога, отсчётов.
// Около 1 мс при 48 кГц канала: короткий импульс виден, а одиночный гребень
// шума почти не поднимает мощность окна над средним.
const peakWindow = 48

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
// тише порога, и хвост кадра теряется.
//
// Открытие — по средней мощности или по короткому окну: импульс маяка/брелока часто
// короче блока, и одна средняя мощность его не видит.
type Squelch struct {
	ThresholdDBFS float64
	HoldSamples   int
	level         float32
	peak          float32
	holdLeft      int
}

// Open измеряет уровень и пик блока канала и сообщает, открыт ли порог для этого блока.
func (s *Squelch) Open(iq []complex64) bool {
	s.level = LevelDBFS(iq)
	s.peak = PeakDBFS(iq)
	if s.AboveThreshold() {
		s.holdLeft = s.HoldSamples
		return true
	}
	if s.holdLeft > 0 {
		s.holdLeft -= len(iq)
		return true
	}
	return false
}

// AboveThreshold — этот блок громче порога по средней или по короткому окну (без удержания).
func (s *Squelch) AboveThreshold() bool {
	th := s.ThresholdDBFS
	return float64(s.level) >= th || float64(s.peak) >= th
}

// LevelDBFS — средняя мощность последнего блока, переданного в Open.
func (s *Squelch) LevelDBFS() float32 {
	return s.level
}

// PeakDBFS — пик короткого окна последнего блока, переданного в Open.
func (s *Squelch) PeakDBFS() float32 {
	return s.peak
}
