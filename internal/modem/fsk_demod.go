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
	// fskSwingGain — вес символа в оценке девиации: около 256 символов памяти.
	fskSwingGain = 1.0 / 256
	// fskHistLen — окно последних символов для быстрого восстановления порога после шума,
	// FM-кликов или скачка сдвига несущей.
	fskHistLen = 10
	// fskHistSpanMin — минимальный размах частоты в окне (в долях девиации),
	// отличающий двухпозиционный FSK от несущей и длинной серии одинаковых бит.
	fskHistSpanMin = 0.8
	// fskBimodalMargin — отступ от середины окна (доля размаха), за которым символ
	// считается уверенным уровнем 0 или 1 при проверке бимодальности окна.
	fskBimodalMargin = 0.15
	// fskBimodalMinPerSide — минимум символов в нижней и верхней частях окна.
	fskBimodalMinPerSide = 2
	// fskBimodalMinCross — минимум переходов через середину окна.
	fskBimodalMinCross = 1
	// fskLevelOutlierMargin — насколько уровень high или low может отстоять за пределами
	// бимодального окна (в долях его размаха), прежде чем считаться артефактом шума/клика.
	fskLevelOutlierMargin = 1.2
)

// FSKDemod — некогерентный демодулятор двухпозиционной FM (FSK, GFSK, GMSK): частотный
// дискриминатор, усреднение по длине символа, цифровая ФАПЧ тактовой частоты по переходам.
// Остаток несущей сдвигает выход дискриминатора, поэтому порог решения следит за серединой
// между уровнями 1 и 0 по принятым символам и быстро перепривязывается, если после шума,
// FM-кликов или большого сдвига несущей порог оказался на краю или вне глаза.
// На входе — IQ уже выделенного канала, на выходе — уровни линии 0/1 (до дескремблера).
// Состояние сохраняется между вызовами Demodulate. Не потокобезопасен.
type FSKDemod struct {
	prev     complex128
	havePrev bool
	norm     float64 // перевод радиан на отсчёт в доли девиации

	ring []float64 // выход дискриминатора в окне усреднения
	sum  float64
	pos  int

	high, low float64 // уровни 1 и 0
	threshold float64 // середина между уровнями
	frozen    bool    // внешняя заморозка адаптации на хвосте шумоподавителя

	yHist     [fskHistLen]float64 // последние отсчёты решения для быстрого восстановления порога
	histPos   int
	histCount int

	clock     float64 // фаза символа в долях: отсчёт решения на переходе через 1
	clockStep float64
	lastHigh  bool
	jitter    float64 // средняя ошибка фазы тактов на переходах, доли символа

	deviationHz float64
	swing       float64 // среднее отклонение частоты от порога в середине символа, доли девиации
}

// NewFSKDemod создаёт демодулятор; cfg.SampleRate — частота дискретизации канала.
func NewFSKDemod(cfg FSKConfig) *FSKDemod {
	window := max(int(math.Round(cfg.SampleRate/cfg.Baud)), 1)
	return &FSKDemod{
		norm:        cfg.SampleRate / (twoPi * cfg.DeviationHz),
		ring:        make([]float64, window),
		clockStep:   cfg.Baud / cfg.SampleRate,
		jitter:      jitterNoLock,
		deviationHz: cfg.DeviationHz,
	}
}

// SetFreeze замораживает или размораживает адаптацию порогов решения (например,
// на хвосте удержания шумоподавителя, когда в канале остался шум).
func (d *FSKDemod) SetFreeze(freeze bool) { d.frozen = freeze }

// DeviationHz — принятая девиация: среднее отклонение мгновенной частоты от порога
// в середине символа. Уровни решения для этого не годятся: усреднение по символу
// держит их внутри глаза. Гауссово сглаживание даёт чуть меньше девиации передатчика.
func (d *FSKDemod) DeviationHz() float64 {
	return d.swing * d.deviationHz
}

// ClockJitter — средняя ошибка фазы тактов на переходах уровня, в долях символа:
// у захваченного сигнала доли процента, на шуме около четверти символа.
func (d *FSKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает в dst принятые уровни линии и возвращает расширенный срез.
func (d *FSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	nWin := float64(len(d.ring))
	for _, s := range iq {
		x := complex128(s)
		if !d.havePrev {
			// Без предыдущего отсчёта разность фаз с нулём даёт выброс и портит преамбулу
			// на большом окне (низкая скорость, много отсчётов на символ).
			d.prev, d.havePrev = x, true
			continue
		}
		f := cmplx.Phase(x*cmplx.Conj(d.prev)) * d.norm
		d.prev = x

		d.sum += f - d.ring[d.pos]
		d.ring[d.pos] = f
		d.pos = (d.pos + 1) % len(d.ring)
		y := d.sum / nWin

		// Переход уровня должен приходиться на середину между отсчётами решения.
		high := y > d.threshold
		if high != d.lastHigh {
			d.jitter = trackJitter(d.jitter, d.clock)
			d.clock += fskClockGain * (0.5 - d.clock)
			d.lastHigh = high
		}

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--

		if d.frozen {
			d.histCount = 0
		} else {
			high = d.adaptLevels(y, high)
		}

		// Окно усреднения сейчас лежит на символе: его середина — середина символа.
		mid := d.ring[(d.pos+len(d.ring)/2)%len(d.ring)]
		d.swing += fskSwingGain * (math.Abs(mid-d.threshold) - d.swing)
		bit := byte(0)
		if high {
			bit = 1
		}
		dst = append(dst, bit)
	}
	return dst
}

// adaptLevels обновляет уровни 1/0 и при необходимости перепривязывает порог к бимодальному
// окну последних символов, если один из уровней ушёл далеко после шума, кликов или сдвига несущей.
func (d *FSKDemod) adaptLevels(y float64, high bool) bool {
	d.yHist[d.histPos] = y
	d.histPos = (d.histPos + 1) % fskHistLen
	if d.histCount < fskHistLen {
		d.histCount++
	}

	d.high = trackLevel(d.high, y, y > d.high)
	d.low = trackLevel(d.low, y, y < d.low)
	d.threshold = (d.high + d.low) / 2

	if d.histCount < fskHistLen {
		return high
	}
	lo, hi, ok := d.bimodalWindow()
	if !ok {
		return high
	}
	margin := fskLevelOutlierMargin * (hi - lo)
	changed := false
	if d.high > hi+margin || d.high < lo {
		d.high = hi
		changed = true
	}
	if d.low < lo-margin || d.low > hi {
		d.low = lo
		changed = true
	}
	if changed {
		d.threshold = 0.5 * (d.high + d.low)
		return y > d.threshold
	}
	return high
}

// bimodalWindow проверяет, что последние fskHistLen отсчётов решения образуют
// устойчивый двухпозиционный глаз (достаточный размах, символы по обе стороны и переходы).
func (d *FSKDemod) bimodalWindow() (float64, float64, bool) {
	start := d.histPos
	lo, hi := d.yHist[start], d.yHist[start]
	for i := 1; i < fskHistLen; i++ {
		v := d.yHist[(start+i)%fskHistLen]
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	span := hi - lo
	if span < fskHistSpanMin {
		return 0, 0, false
	}
	mid := 0.5 * (lo + hi)
	loCut := mid - fskBimodalMargin*span
	hiCut := mid + fskBimodalMargin*span
	var nLo, nHi, crosses int
	prevAbove := d.yHist[start] > mid
	for i := range fskHistLen {
		v := d.yHist[(start+i)%fskHistLen]
		if v <= loCut {
			nLo++
		} else if v >= hiCut {
			nHi++
		}
		above := v > mid
		if i > 0 && above != prevAbove {
			crosses++
			prevAbove = above
		}
	}
	if nLo < fskBimodalMinPerSide || nHi < fskBimodalMinPerSide || crosses < fskBimodalMinCross {
		return 0, 0, false
	}
	return lo, hi, true
}

// trackLevel подтягивает уровень к отсчёту: быстро, если отсчёт за уровнем, иначе медленно.
func trackLevel(level, y float64, outside bool) float64 {
	if outside {
		return level + fskLevelAttack*(y-level)
	}
	return level + fskLevelDecay*(y-level)
}
