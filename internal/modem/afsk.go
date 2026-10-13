package modem

import "math"

const twoPi = 2 * math.Pi

// AFSKConfig — параметры AFSK поверх узкополосной FM.
type AFSKConfig struct {
	SampleRate  float64 // частота дискретизации IQ, Гц
	Baud        float64 // символьная скорость, Бод
	MarkHz      float64 // тон уровня 1
	SpaceHz     float64 // тон уровня 0
	DeviationHz float64 // пиковая девиация FM, Гц
}

// AFSK1200 — Bell 202 (1200/2200 Гц, 1200 Бод) с девиацией 3 кГц, как у маяков КА на 145/435 МГц.
func AFSK1200(sampleRate float64) AFSKConfig {
	return AFSKConfig{
		SampleRate:  sampleRate,
		Baud:        1200,
		MarkHz:      1200,
		SpaceHz:     2200,
		DeviationHz: 3000,
	}
}

// AFSK — модулятор с непрерывной фазой: состояние сохраняется между вызовами Modulate,
// поэтому кадр можно модулировать по частям.
type AFSK struct {
	cfg        AFSKConfig
	audioPhase float64
	rfPhase    float64
	symbols    int // символов промодулировано с начала
	emitted    int // отсчётов выдано с начала
}

// NewAFSK создаёт модулятор.
func NewAFSK(cfg AFSKConfig) *AFSK {
	return &AFSK{cfg: cfg}
}

// Modulate дописывает в dst отсчёты для уровней линии (0 или 1) и возвращает расширенный срез.
func (m *AFSK) Modulate(dst []complex64, levels []byte) []complex64 {
	samplesPerSymbol := m.cfg.SampleRate / m.cfg.Baud
	markInc := twoPi * m.cfg.MarkHz / m.cfg.SampleRate
	spaceInc := twoPi * m.cfg.SpaceHz / m.cfg.SampleRate
	devGain := twoPi * m.cfg.DeviationHz / m.cfg.SampleRate

	for _, level := range levels {
		audioInc := spaceInc
		if level != 0 {
			audioInc = markInc
		}

		// Граница символа — от его номера, а не накоплением: ошибка округления не копится.
		m.symbols++
		end := int(math.Round(float64(m.symbols) * samplesPerSymbol))
		n := end - m.emitted
		m.emitted = end

		for range n {
			m.audioPhase = math.Mod(m.audioPhase+audioInc, twoPi)
			m.rfPhase = math.Mod(m.rfPhase+devGain*math.Sin(m.audioPhase), twoPi)
			sin, cos := math.Sincos(m.rfPhase)
			dst = append(dst, complex(float32(cos), float32(sin)))
		}
	}
	return dst
}
