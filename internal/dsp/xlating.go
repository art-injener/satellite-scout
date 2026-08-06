package dsp

import "math"

// DesignLowpass рассчитывает коэффициенты FIR-фильтра нижних частот
// методом оконного sinc. Если numTaps чётное — увеличивается на 1
// (симметричный фильтр с линейной фазой требует нечётное число).
// DC gain нормализуется к 1.
func DesignLowpass(cutoffHz, sampleRate float64, numTaps int, wt WindowType) []float64 {
	if numTaps%2 == 0 {
		numTaps++
	}

	fc := cutoffHz / sampleRate // нормированная частота среза
	mid := (numTaps - 1) / 2

	// Для FIR нужно симметричное окно (делитель N-1, не N как для FFT)
	win := makeSymmetricWindow(numTaps, wt)
	taps := make([]float64, numTaps)

	for n := 0; n < numTaps; n++ {
		x := float64(n - mid)
		if n == mid {
			taps[n] = 2 * fc
		} else {
			taps[n] = math.Sin(2*math.Pi*fc*x) / (math.Pi * x)
		}
		taps[n] *= win[n]
	}

	// DC gain = 1
	sum := 0.0
	for _, v := range taps {
		sum += v
	}
	for i := range taps {
		taps[i] /= sum
	}

	return taps
}

// XlatingFIR сдвигает частоту, фильтрует и децимирует IQ-поток.
// Типичное применение: из широкой полосы SDR (2.4 МSPS) выделить
// узкий канал (25 кГц) на заданной частоте.
//
// Алгоритм адаптирован из sdr-server (Apache-2.0):
//
//	https://github.com/dernasherbrezon/sdr-server
//	src/dsp.c — xlating_filter
type XlatingFIR struct {
	sampleRate float64
	decimation int
	taps       []float64
	phase      float64 // текущая фаза NCO (рад)
	phaseStep  float64 // приращение фазы за отсчёт
	delay      []complex128
	delayPos   int
	count      int // счётчик отсчётов до следующей точки децимации
}

// NewXlatingFIR создаёт фильтр.
//   - shiftHz — на какую частоту настроиться (смещение от центра SDR)
//   - sampleRate — входной sample rate
//   - decimation — во сколько раз снизить sample rate
//   - taps — коэффициенты lowpass FIR (см. DesignLowpass)
func NewXlatingFIR(shiftHz, sampleRate float64, decimation int, taps []float64) *XlatingFIR {
	return &XlatingFIR{
		sampleRate: sampleRate,
		decimation: decimation,
		taps:       taps,
		phaseStep:  -2.0 * math.Pi * shiftHz / sampleRate,
		delay:      make([]complex128, len(taps)),
	}
}

// Process пропускает блок IQ через сдвиг → фильтр → децимацию.
// Состояние (фаза NCO, линия задержки) сохраняется между вызовами.
func (x *XlatingFIR) Process(in []complex64) []complex64 {
	out := make([]complex64, 0, len(in)/x.decimation+1)
	ntaps := len(x.taps)

	for _, s := range in {
		// NCO: сдвигаем частоту
		sin, cos := math.Sincos(x.phase)
		shifted := complex128(s) * complex(cos, sin)
		x.phase += x.phaseStep

		// Фаза в [-π, π] — иначе теряем точность на длинных потоках
		if x.phase > math.Pi {
			x.phase -= 2 * math.Pi
		} else if x.phase < -math.Pi {
			x.phase += 2 * math.Pi
		}

		// Записываем в кольцевую линию задержки FIR
		x.delay[x.delayPos] = shifted
		x.delayPos++
		if x.delayPos >= ntaps {
			x.delayPos = 0
		}

		x.count++
		if x.count < x.decimation {
			continue
		}
		x.count = 0

		// Свёртка с FIR только на точках децимации (экономим в D раз)
		var acc complex128
		pos := x.delayPos
		for k := 0; k < ntaps; k++ {
			pos--
			if pos < 0 {
				pos = ntaps - 1
			}
			acc += x.delay[pos] * complex(x.taps[k], 0)
		}
		out = append(out, complex64(acc))
	}

	return out
}

// SetShift перестраивает NCO на новую частоту без сброса фильтра.
func (x *XlatingFIR) SetShift(hz float64) {
	x.phaseStep = -2.0 * math.Pi * hz / x.sampleRate
}

// OutputRate возвращает sample rate после децимации.
func (x *XlatingFIR) OutputRate() float64 {
	return x.sampleRate / float64(x.decimation)
}
