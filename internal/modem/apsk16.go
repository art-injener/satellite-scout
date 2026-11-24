package modem

import "math"

const (
	// DefaultAPSK16Baud — символьная скорость профиля apsk16, если baud канала не задан.
	DefaultAPSK16Baud = 9600.0
	// apsk16Gamma — отношение радиусов внешнего и внутреннего кольца (DVB-S2 16APSK, R=2/3).
	apsk16Gamma = 2.57
)

// APSK16Config — 16-APSK (4+12): четыре бита на символ.
type APSK16Config struct {
	SampleRate float64
	Baud       float64 // символов/с; 0 — DefaultAPSK16Baud
}

// APSK16BaudOrDefault возвращает baud канала или DefaultAPSK16Baud.
func APSK16BaudOrDefault(baud float64) float64 {
	if baud > 0 {
		return baud
	}
	return DefaultAPSK16Baud
}

// apsk16Points — нормированное созвездие: внутр. кольцо 4 точки, внешнее 12.
// Индекс 0…15 соответствует битам b0 b1 b2 b3 (b0 старший).
var apsk16Points [16]complex128

func init() {
	// Мощность ≈1: 4·r1² + 12·r2² = 16, r2 = γ·r1.
	r1 := math.Sqrt(16 / (4 + 12*apsk16Gamma*apsk16Gamma))
	r2 := apsk16Gamma * r1
	// Внутреннее кольцо: π/4 + k·π/2, индексы 0…3.
	for k := 0; k < 4; k++ {
		ph := math.Pi/4 + float64(k)*math.Pi/2
		apsk16Points[k] = complex(r1*math.Cos(ph), r1*math.Sin(ph))
	}
	// Внешнее кольцо: 12 точек, старт с π/12, шаг π/6; индексы 4…15.
	for k := 0; k < 12; k++ {
		ph := math.Pi/12 + float64(k)*math.Pi/6
		apsk16Points[4+k] = complex(r2*math.Cos(ph), r2*math.Sin(ph))
	}
}

// APSK16 — модулятор 16-APSK. Биты пакуются по четыре; короткий хвост дополняется нулями.
type APSK16 struct {
	cfg     APSK16Config
	symbols int
	emitted int
}

// NewAPSK16 создаёт модулятор.
func NewAPSK16(cfg APSK16Config) *APSK16 {
	cfg.Baud = APSK16BaudOrDefault(cfg.Baud)
	return &APSK16{cfg: cfg}
}

// Modulate дописывает IQ для уровней линии 0/1 и возвращает расширенный срез.
func (m *APSK16) Modulate(dst []complex64, levels []byte) []complex64 {
	sps := m.cfg.SampleRate / m.cfg.Baud
	for i := 0; i+3 < len(levels); i += 4 {
		idx := int(levels[i])<<3 | int(levels[i+1])<<2 | int(levels[i+2])<<1 | int(levels[i+3])
		c := complex64(apsk16Points[idx&15])
		m.symbols++
		end := int(math.Round(float64(m.symbols) * sps))
		for ; m.emitted < end; m.emitted++ {
			dst = append(dst, c)
		}
	}
	return dst
}

// apsk16Nearest — индекс ближайшей точки созвездия.
func apsk16Nearest(z complex128) int {
	best, bestD := 0, cmplxAbs2(z-apsk16Points[0])
	for i := 1; i < 16; i++ {
		if d := cmplxAbs2(z - apsk16Points[i]); d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

func cmplxAbs2(z complex128) float64 {
	return real(z)*real(z) + imag(z)*imag(z)
}

func apsk16IndexBits(idx int) (b0, b1, b2, b3 byte) {
	return byte((idx >> 3) & 1), byte((idx >> 2) & 1), byte((idx >> 1) & 1), byte(idx & 1)
}
