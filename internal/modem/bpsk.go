package modem

import "math"

// BPSKConfig — двоичная ФМ: уровни линии после дифференциального кодирования
// становятся сменой фазы (1) или её удержанием (0). Так снимается неоднозначность 180°.
type BPSKConfig struct {
	SampleRate float64 // частота дискретизации IQ, Гц
	Baud       float64 // символьная скорость, Бод
}

// BPSK1200 — BPSK 1200 Бод (любительский UHF/VHF, как в gr-satellites).
func BPSK1200(sampleRate float64) BPSKConfig {
	return BPSKConfig{SampleRate: sampleRate, Baud: 1200}
}

// BPSK9600 — BPSK 9600 Бод.
func BPSK9600(sampleRate float64) BPSKConfig {
	return BPSKConfig{SampleRate: sampleRate, Baud: 9600}
}

// BPSK — модулятор с дифференциальным кодированием; состояние фазы между вызовами.
type BPSK struct {
	cfg     BPSKConfig
	phase   float64 // 0 или π
	symbols int
	emitted int
}

// NewBPSK создаёт модулятор.
func NewBPSK(cfg BPSKConfig) *BPSK {
	return &BPSK{cfg: cfg}
}

// Modulate дописывает в dst отсчёты для уровней линии (0 или 1) и возвращает расширенный срез.
// Уровень 1 — смена фазы на π, 0 — фаза прежняя (DBPSK).
func (m *BPSK) Modulate(dst []complex64, levels []byte) []complex64 {
	sps := m.cfg.SampleRate / m.cfg.Baud
	for _, level := range levels {
		if level != 0 {
			m.phase = math.Mod(m.phase+math.Pi, 2*math.Pi)
		}
		m.symbols++
		end := int(math.Round(float64(m.symbols) * sps))
		re, im := float32(math.Cos(m.phase)), float32(math.Sin(m.phase))
		for ; m.emitted < end; m.emitted++ {
			dst = append(dst, complex(re, im))
		}
	}
	return dst
}
