package modem

import (
	"math"
	"math/cmplx"
)

// afskClockGain — доля ошибки фазы символа, снимаемая на каждом переходе уровня.
const afskClockGain = 0.3

// AFSKDemod — демодулятор AFSK поверх FM: дискриминатор, некогерентные корреляторы
// mark/space на длину символа и цифровая ФАПЧ тактовой частоты по переходам.
// На входе — IQ уже выделенного канала (несущая около нуля), на выходе — уровни
// линии 0/1. Остаток несущей в несколько килогерц после дискриминатора даёт
// постоянную составляющую звука, а корреляторы на длину символа её почти не видят,
// поэтому отдельно она не вычитается. Состояние сохраняется между вызовами
// Demodulate. Не потокобезопасен.
type AFSKDemod struct {
	prev complex128

	markPhase, spacePhase float64
	markInc, spaceInc     float64
	markRing, spaceRing   []complex128 // вклады отсчётов в окне коррелятора
	markSum, spaceSum     complex128
	pos                   int

	clock     float64 // фаза символа в долях: отсчёт решения на переходе через 1
	clockStep float64
	lastMark  bool
}

// NewAFSKDemod создаёт демодулятор; cfg.SampleRate — частота дискретизации канала.
func NewAFSKDemod(cfg AFSKConfig) *AFSKDemod {
	window := max(int(math.Round(cfg.SampleRate/cfg.Baud)), 1)
	return &AFSKDemod{
		markInc:   twoPi * cfg.MarkHz / cfg.SampleRate,
		spaceInc:  twoPi * cfg.SpaceHz / cfg.SampleRate,
		markRing:  make([]complex128, window),
		spaceRing: make([]complex128, window),
		clockStep: cfg.Baud / cfg.SampleRate,
	}
}

// Demodulate дописывает в dst принятые уровни линии и возвращает расширенный срез.
func (d *AFSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		// Мгновенная частота в радианах на отсчёт; от амплитуды не зависит.
		a := cmplx.Phase(x * cmplx.Conj(d.prev))
		d.prev = x

		sin, cos := math.Sincos(d.markPhase)
		m := complex(a*cos, -a*sin)
		sin, cos = math.Sincos(d.spacePhase)
		sp := complex(a*cos, -a*sin)
		d.markSum += m - d.markRing[d.pos]
		d.spaceSum += sp - d.spaceRing[d.pos]
		d.markRing[d.pos], d.spaceRing[d.pos] = m, sp
		d.pos = (d.pos + 1) % len(d.markRing)
		d.markPhase = math.Mod(d.markPhase+d.markInc, twoPi)
		d.spacePhase = math.Mod(d.spacePhase+d.spaceInc, twoPi)

		// Переход уровня должен приходиться на середину между отсчётами решения.
		mark := cmplx.Abs(d.markSum) > cmplx.Abs(d.spaceSum)
		if mark != d.lastMark {
			d.clock += afskClockGain * (0.5 - d.clock)
			d.lastMark = mark
		}

		d.clock += d.clockStep
		if d.clock >= 1 {
			d.clock--
			var bit byte
			if mark {
				bit = 1
			}
			dst = append(dst, bit)
		}
	}
	return dst
}
