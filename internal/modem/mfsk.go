package modem

import (
	"math"
	"math/cmplx"
)

// MFSKConfig — многопозиционная ЧМ (3FSK / 4FSK / GMFSK): M уровней частоты
// с шагом StepHz вокруг нуля. BT — гауссов фильтр; большой BT ≈ прямоугольный импульс.
type MFSKConfig struct {
	SampleRate float64
	Baud       float64
	Levels     int     // число частот: 3 или 4
	StepHz     float64 // шаг между соседними уровнями, Гц
	BT         float64 // произведение полосы гаусса на длительность символа
}

// toneMultipliers — множители StepHz для уровней 0…Levels-1 (симметрично вокруг 0).
// 3FSK: −1, 0, +1; 4FSK (DMR/C4FM): −3, −1, +1, +3.
func toneMultipliers(levels int) []float64 {
	switch levels {
	case 3:
		return []float64{-1, 0, 1}
	case 4:
		return []float64{-3, -1, 1, 3}
	default:
		return nil
	}
}

// FSK3_4800 — 3FSK 4800 Бод, шаг ±2,4 кГц (уровни −2,4 / 0 / +2,4 кГц).
func FSK3_4800(sampleRate float64) MFSKConfig {
	return MFSKConfig{SampleRate: sampleRate, Baud: 4800, Levels: 3, StepHz: 2400, BT: mskNearRectBT}
}

// FSK4DMR4800 — 4FSK как у DMR/C4FM: 4800 симв/с, шаг ≈648 Гц (тона ±648 и ±1944 Гц).
// Кадр и слоты DMR сюда не входят — только линия.
func FSK4DMR4800(sampleRate float64) MFSKConfig {
	return MFSKConfig{SampleRate: sampleRate, Baud: 4800, Levels: 4, StepHz: 648, BT: mskNearRectBT}
}

// GMFSK4800 — 4FSK с гауссовым сглаживанием (GMFSK), тона как у DMR-подобной.
// BT 1,0: на некогерентном MFSKDemod классические 0,3–0,5 дают сильный ISI между
// соседними уровнями (±648 Гц); для бинарного GFSK BT 0,5 достаточен.
func GMFSK4800(sampleRate float64) MFSKConfig {
	return MFSKConfig{SampleRate: sampleRate, Baud: 4800, Levels: 4, StepHz: 648, BT: 1.0}
}

// MFSK — модулятор M-уровневой ЧМ с непрерывной фазой (как FSK, но уровень 0…M−1).
type MFSK struct {
	cfg     MFSKConfig
	tones   []float64 // множители StepHz по индексу уровня
	pulse   []float64
	hist    [fskHistory]float64
	phase   float64
	symbols int
	emitted int
}

// NewMFSK создаёт модулятор; Levels должен быть 3 или 4.
func NewMFSK(cfg MFSKConfig) *MFSK {
	tones := toneMultipliers(cfg.Levels)
	if tones == nil {
		tones = toneMultipliers(4)
		cfg.Levels = 4
	}
	sigma := math.Sqrt(math.Ln2) / (2 * math.Pi * cfg.BT)
	k := 1 / (math.Sqrt2 * sigma)
	n := 2*fskPulseHalfSpan*fskPulseRes + 1
	pulse := make([]float64, n)
	for i := range pulse {
		dt := float64(i)/fskPulseRes - fskPulseHalfSpan
		pulse[i] = 0.5 * (math.Erf(k*(dt+0.5)) - math.Erf(k*(dt-0.5)))
	}
	return &MFSK{cfg: cfg, tones: tones, pulse: pulse}
}

// Modulate дописывает IQ для уровней линии (0…Levels−1) и возвращает расширенный срез.
func (m *MFSK) Modulate(dst []complex64, levels []byte) []complex64 {
	samplesPerSymbol := m.cfg.SampleRate / m.cfg.Baud
	devGain := twoPi * m.cfg.StepHz / m.cfg.SampleRate
	nTone := len(m.tones)

	for _, level := range levels {
		idx := int(level)
		if idx >= nTone {
			idx = nTone - 1
		}
		m.hist[m.symbols%fskHistory] = m.tones[idx]
		m.symbols++

		end := int(math.Round(float64(m.symbols) * samplesPerSymbol))
		for ; m.emitted < end; m.emitted++ {
			m.phase = math.Mod(m.phase+devGain*m.frequency(float64(m.emitted)/samplesPerSymbol), twoPi)
			sin, cos := math.Sincos(m.phase)
			dst = append(dst, complex(float32(cos), float32(sin)))
		}
	}
	return dst
}

func (m *MFSK) frequency(t float64) float64 {
	j := int(math.Floor(t))
	f := 0.0
	for k := max(j-2*fskPulseHalfSpan, 0); k <= j && k < m.symbols; k++ {
		dt := t - (float64(k) + 0.5 + fskDelaySymbols)
		if math.Abs(dt) >= fskPulseHalfSpan {
			continue
		}
		f += m.hist[k%fskHistory] * m.pulse[int(math.Round((dt+fskPulseHalfSpan)*fskPulseRes))]
	}
	return f
}

// MFSKDemod — некогерентный приём M-FSK: дискриминатор, усреднение по символу,
// решение к ближайшему уровню. Состояние между вызовами. Не потокобезопасен.
type MFSKDemod struct {
	prev     complex128
	havePrev bool
	norm     float64 // радиан/отсчёт → доли StepHz

	ring []float64
	sum  float64
	pos  int

	tones     []float64
	clock     float64
	clockStep float64
	jitter    float64
	lastIdx   int
	haveY     bool
}

// NewMFSKDemod создаёт демодулятор.
func NewMFSKDemod(cfg MFSKConfig) *MFSKDemod {
	tones := toneMultipliers(cfg.Levels)
	if tones == nil {
		tones = toneMultipliers(4)
	}
	window := max(int(math.Round(cfg.SampleRate/cfg.Baud)), 1)
	return &MFSKDemod{
		norm:      cfg.SampleRate / (twoPi * cfg.StepHz),
		ring:      make([]float64, window),
		tones:     tones,
		clockStep: cfg.Baud / cfg.SampleRate,
		jitter:    jitterNoLock,
	}
}

// ClockJitter — оценка джиттера тактов; на M-FSK грубее, чем у бинарного FSK.
func (d *MFSKDemod) ClockJitter() float64 { return d.jitter }

// Demodulate дописывает уровни 0…M−1 и возвращает расширенный срез.
func (d *MFSKDemod) Demodulate(dst []byte, iq []complex64) []byte {
	for _, s := range iq {
		x := complex128(s)
		if !d.havePrev {
			d.prev, d.havePrev = x, true
			continue
		}
		f := cmplx.Phase(x*cmplx.Conj(d.prev)) * d.norm
		d.prev = x

		d.sum += f - d.ring[d.pos]
		d.ring[d.pos] = f
		d.pos = (d.pos + 1) % len(d.ring)
		y := d.sum / float64(len(d.ring))
		idx := nearestTone(y, d.tones)

		if d.haveY && idx != d.lastIdx {
			d.jitter = trackJitter(d.jitter, d.clock)
			d.clock += fskClockGain * (0.5 - d.clock)
		}
		d.lastIdx, d.haveY = idx, true

		d.clock += d.clockStep
		if d.clock < 1 {
			continue
		}
		d.clock--
		dst = append(dst, byte(idx))
	}
	return dst
}

func nearestTone(y float64, tones []float64) int {
	best, dist := 0, math.Abs(y-tones[0])
	for i := 1; i < len(tones); i++ {
		if d := math.Abs(y - tones[i]); d < dist {
			best, dist = i, d
		}
	}
	return best
}
