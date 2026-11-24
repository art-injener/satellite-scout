package modem

import "math"

const (
	// DefaultPI4DQPSKBaud — символьная скорость профиля pi4_dqpsk, если baud канала не задан.
	DefaultPI4DQPSKBaud = 9600.0
)

// PI4DQPSKConfig — π/4-DQPSK: два бита на символ, дифференц. сдвиг фазы.
type PI4DQPSKConfig struct {
	SampleRate float64
	Baud       float64 // символов/с; 0 — DefaultPI4DQPSKBaud
}

// PI4DQPSKBaudOrDefault возвращает baud канала или DefaultPI4DQPSKBaud.
func PI4DQPSKBaudOrDefault(baud float64) float64 {
	if baud > 0 {
		return baud
	}
	return DefaultPI4DQPSKBaud
}

// pi4PhaseDelta — сдвиг фазы по дибиту Gray: 00→+π/4, 01→+3π/4, 11→−3π/4, 10→−π/4.
func pi4PhaseDelta(b0, b1 byte) float64 {
	switch {
	case b0 == 0 && b1 == 0:
		return math.Pi / 4
	case b0 == 0 && b1 != 0:
		return 3 * math.Pi / 4
	case b0 != 0 && b1 != 0:
		return -3 * math.Pi / 4
	default:
		return -math.Pi / 4
	}
}

// PI4DQPSK — модулятор π/4-DQPSK. Биты пакуются по два; нечётный хвост — 0.
type PI4DQPSK struct {
	cfg     PI4DQPSKConfig
	phase   float64
	symbols int
	emitted int
}

// NewPI4DQPSK создаёт модулятор.
func NewPI4DQPSK(cfg PI4DQPSKConfig) *PI4DQPSK {
	cfg.Baud = PI4DQPSKBaudOrDefault(cfg.Baud)
	return &PI4DQPSK{cfg: cfg}
}

// Modulate дописывает IQ для уровней линии 0/1 и возвращает расширенный срез.
func (m *PI4DQPSK) Modulate(dst []complex64, levels []byte) []complex64 {
	sps := m.cfg.SampleRate / m.cfg.Baud
	for i := 0; i+1 < len(levels); i += 2 {
		m.phase = math.Mod(m.phase+pi4PhaseDelta(levels[i], levels[i+1]), twoPi)
		c := complex(float32(math.Cos(m.phase)), float32(math.Sin(m.phase)))
		m.symbols++
		end := int(math.Round(float64(m.symbols) * sps))
		for ; m.emitted < end; m.emitted++ {
			dst = append(dst, c)
		}
	}
	return dst
}
