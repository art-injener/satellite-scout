package modem

import (
	"math"
	"math/cmplx"
)

const (
	// fskClockGain — доля ошибки фазы символа, снимаемая на каждом переходе уровня.
	fskClockGain = 0.3
	// Уровни 1 и 0 следят за выходом в отсчётах решения: к значению за пределами уровня
	// подтягиваются быстро, к значению внутри медленно спадают. Порог устанавливается
	// за несколько десятков символов, то есть в преамбуле.
	fskLevelAttack = 1.0 / 4
	fskLevelDecay  = 1.0 / 64
)

// FSKDemod — некогерентный демодулятор двухпозиционной FM (FSK, GFSK, GMSK): частотный
// дискриминатор, усреднение по длине символа, цифровая ФАПЧ тактовой частоты по переходам.
// Остаток несущей сдвигает выход дискриминатора на долю девиации, поэтому порог решения
// следит за серединой между уровнями 1 и 0 по принятым символам. На входе — IQ уже
// выделенного канала, на выходе — уровни линии 0/1 (до дескремблера). Состояние
// сохраняется между вызовами Demodulate. Не потокобезопасен.
type FSKDemod struct {
	prev complex128
	norm float64 // перевод радиан на отсчёт в доли девиации

	ring []float64 // выход дискриминатора в окне усреднения
	sum  float64
	pos  int

	high, low float64 // уровни 1 и 0
	threshold float64 // середина между уровнями

	clock     float64 // фаза символа в долях: отсчёт решения на переходе через 1
	clockStep float64
	lastHigh  bool
}

// NewFSKDemod создаёт демодулятор; cfg.SampleRate — частота дискретизации канала.
func NewFSKDemod(cfg FSKConfig) *FSKDemod {
	window := max(int(math.Round(cfg.SampleRate/cfg.Baud)), 1)
	return &FSKDemod{
		norm:      cfg.SampleRate / (twoPi * cfg.DeviationHz),
		ring:      make([]float64, window),
		clockStep: cfg.Baud / cfg.SampleRate,
	}
}

// Demodulate дописывает в dst принятые уровни линии и возвращает расширенный срез.
func (d *FSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		f := cmplx.Phase(x*cmplx.Conj(d.prev)) * d.norm
		d.prev = x

		d.sum += f - d.ring[d.pos]
		d.ring[d.pos] = f
		d.pos = (d.pos + 1) % len(d.ring)
		y := d.sum / float64(len(d.ring))

		// Переход уровня должен приходиться на середину между отсчётами решения.
		high := y > d.threshold
		if high != d.lastHigh {
			d.clock += fskClockGain * (0.5 - d.clock)
			d.lastHigh = high
		}

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		d.high = trackLevel(d.high, y, y > d.high)
		d.low = trackLevel(d.low, y, y < d.low)
		d.threshold = (d.high + d.low) / 2
		bit := byte(0)
		if high {
			bit = 1
		}
		dst = append(dst, bit)
	}
	return dst
}

// trackLevel подтягивает уровень к отсчёту: быстро, если отсчёт за уровнем, иначе медленно.
func trackLevel(level, y float64, outside bool) float64 {
	if outside {
		return level + fskLevelAttack*(y-level)
	}
	return level + fskLevelDecay*(y-level)
}
