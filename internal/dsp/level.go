package dsp

import "math"

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

// Squelch — порог демодулятора: блок канала, тише ThresholdDBFS, демодулятор не принимает.
// Порог держится в dBFS, как в настройках приёмника. HoldSamples держит порог открытым
// после последнего громкого блока: иначе блок, где сигнал кончается в начале, оказывается
// тише порога, и хвост кадра теряется.
type Squelch struct {
	ThresholdDBFS float64
	HoldSamples   int
	level         float32
	holdLeft      int
}

// Open измеряет уровень блока канала и сообщает, открыт ли порог для этого блока.
func (s *Squelch) Open(iq []complex64) bool {
	s.level = LevelDBFS(iq)
	if float64(s.level) >= s.ThresholdDBFS {
		s.holdLeft = s.HoldSamples
		return true
	}
	if s.holdLeft > 0 {
		s.holdLeft -= len(iq)
		return true
	}
	return false
}

// LevelDBFS — уровень последнего блока, переданного в Open.
func (s *Squelch) LevelDBFS() float32 {
	return s.level
}
