package dsp

import "math"

// dcCutoffHz — срез фильтра постоянной составляющей. Уже нельзя: оценка среднего
// не успевает за уходом нуля при перестройке. Шире не нужно: полезный сигнал
// стоит на сдвиге гетеродина, в десятках килогерц от нуля.
const dcCutoffHz = 100

// DCShift вырезает постоянную и переносит спектр на offsetHz вверх.
// Постоянная снимается до переноса: в ноль попадает частота, которая до
// коррекции стояла на −offsetHz, а не сама постоянная.
type DCShift struct {
	rate     float64
	offsetHz float64
	alpha    float64
	mean     complex128
	phase    float64
	step     float64
}

// NewDCShift создаёт корректор для потока с частотой дискретизации rate.
func NewDCShift(offsetHz, rate float64) *DCShift {
	s := &DCShift{rate: rate, alpha: 1}
	if rate > 0 {
		s.alpha = 1 - math.Exp(-2*math.Pi*dcCutoffHz/rate)
	}
	s.SetOffset(offsetHz)
	return s
}

// SetOffset меняет перенос, не сбрасывая фазу и оценку постоянной.
// Фаза продолжается: скачок фазы дал бы щелчок в звуке и брызги в спектре.
func (s *DCShift) SetOffset(hz float64) {
	if s.offsetHz == hz {
		return
	}
	s.offsetHz = hz
	if s.rate <= 0 {
		s.step = 0
		return
	}
	s.step = 2 * math.Pi * hz / s.rate
}

// Correct обрабатывает блок на месте.
func (s *DCShift) Correct(x []complex64) {
	mean := s.mean
	phase := s.phase
	alpha := s.alpha
	step := s.step
	for i, v := range x {
		c := complex128(v)
		mean += complex(alpha, 0) * (c - mean)
		c -= mean
		sin, cos := math.Sincos(phase)
		c *= complex(cos, sin)
		phase += step
		if phase > math.Pi {
			phase -= 2 * math.Pi
		} else if phase < -math.Pi {
			phase += 2 * math.Pi
		}
		x[i] = complex64(c)
	}
	s.mean = mean
	s.phase = phase
}
