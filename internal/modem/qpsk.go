package modem

import "math"

// QPSKConfig — квадратурная ФМ: два бита на символ, Gray-кодирование.
type QPSKConfig struct {
	SampleRate float64
	Baud       float64 // символьная скорость (символов/с), не бит/с
}

// QPSKCCSDS — QPSK для профиля qpsk_ccsds: 9600 символов/с по умолчанию цепочки.
func QPSKCCSDS(sampleRate float64) QPSKConfig {
	return QPSKConfig{SampleRate: sampleRate, Baud: 9600}
}

// QPSK — модулятор; биты на входе пакуются по два (старший, младший). Нечётный хвост — 0.
type QPSK struct {
	cfg     QPSKConfig
	symbols int
	emitted int
}

// NewQPSK создаёт модулятор.
func NewQPSK(cfg QPSKConfig) *QPSK {
	return &QPSK{cfg: cfg}
}

// Modulate принимает уровни линии 0/1; каждые два бита — один символ созвездия.
func (m *QPSK) Modulate(dst []complex64, levels []byte) []complex64 {
	sps := m.cfg.SampleRate / m.cfg.Baud
	for i := 0; i+1 < len(levels); i += 2 {
		// Gray: 00→(+1,+1), 01→(−1,+1), 11→(−1,−1), 10→(+1,−1), нормировка 1/√2.
		b0, b1 := levels[i], levels[i+1]
		re, im := 1.0, 1.0
		if b0 != 0 {
			re = -1
		}
		if b1 != 0 {
			im = -1
		}
		scale := 1 / math.Sqrt2
		c := complex(float32(re*scale), float32(im*scale))
		m.symbols++
		end := int(math.Round(float64(m.symbols) * sps))
		for ; m.emitted < end; m.emitted++ {
			dst = append(dst, c)
		}
	}
	return dst
}
