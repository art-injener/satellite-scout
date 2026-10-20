package modem

import "math"

const (
	// fskPulseHalfSpan — на сколько символов в каждую сторону учитывается гауссов импульс:
	// при BT 0,5 дальше его вклад меньше 1e-7.
	fskPulseHalfSpan = 2
	// fskDelaySymbols — задержка модулятора: отсчёт строится только по пришедшим символам,
	// поэтому Modulate выдаёт столько же отсчётов, сколько символов получил, как AFSK.
	// Хвост последних символов кадра перекрывают флаги постамбулы.
	fskDelaySymbols = fskPulseHalfSpan
	// fskPulseRes — точек таблицы импульса на символ: шаг 1/1024 символа даёт ошибку
	// частоты меньше 0,1 % девиации.
	fskPulseRes = 1024
	// fskHistory — сколько последних символов хранится: импульс захватывает 2·halfSpan+1.
	fskHistory = 8
)

// FSKConfig — параметры GFSK: двухпозиционная FM с гауссовым сглаживанием частоты.
type FSKConfig struct {
	SampleRate  float64 // частота дискретизации IQ, Гц
	Baud        float64 // символьная скорость, Бод
	DeviationHz float64 // девиация уровня 1 (+) и 0 (−), Гц
	BT          float64 // произведение полосы гауссова фильтра на длительность символа
}

// FSK9600 — FSK 9600 Бод, как у маяков КА на 435 МГц: девиация ±3 кГц, гауссов фильтр BT 0,5.
// Биты на линию — после NRZI и скремблера G3RUH.
func FSK9600(sampleRate float64) FSKConfig {
	return FSKConfig{SampleRate: sampleRate, Baud: 9600, DeviationHz: 3000, BT: 0.5}
}

// FSK — модулятор GFSK с непрерывной фазой: состояние сохраняется между вызовами Modulate.
// Частота — сумма импульсов символов, импульс — прямоугольник символа после гауссова фильтра,
// заранее посчитанный таблицей: свёртка на каждом отсчёте на 1 МГц не успела бы в реальном времени.
type FSK struct {
	cfg     FSKConfig
	pulse   []float64 // импульс на [−halfSpan; +halfSpan] символов с шагом 1/fskPulseRes
	hist    [fskHistory]float64
	phase   float64
	symbols int // символов получено с начала
	emitted int // отсчётов выдано с начала
}

// NewFSK создаёт модулятор.
func NewFSK(cfg FSKConfig) *FSK {
	// Гауссов фильтр с полосой BT по уровню −3 дБ: σ = √(ln 2)/(2π·BT) символа.
	sigma := math.Sqrt(math.Ln2) / (2 * math.Pi * cfg.BT)
	k := 1 / (math.Sqrt2 * sigma)
	n := 2*fskPulseHalfSpan*fskPulseRes + 1
	pulse := make([]float64, n)
	for i := range pulse {
		dt := float64(i)/fskPulseRes - fskPulseHalfSpan
		pulse[i] = 0.5 * (math.Erf(k*(dt+0.5)) - math.Erf(k*(dt-0.5)))
	}
	return &FSK{cfg: cfg, pulse: pulse}
}

// Modulate дописывает в dst отсчёты для уровней линии (0 или 1) и возвращает расширенный срез.
func (m *FSK) Modulate(dst []complex64, levels []byte) []complex64 {
	samplesPerSymbol := m.cfg.SampleRate / m.cfg.Baud
	devGain := twoPi * m.cfg.DeviationHz / m.cfg.SampleRate

	for _, level := range levels {
		a := -1.0
		if level != 0 {
			a = 1
		}
		m.hist[m.symbols%fskHistory] = a
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

// frequency — частота в долях девиации в момент t (в символах от начала).
// Центр символа k — в k + 0,5 + задержка; до первого символа частота нулевая.
func (m *FSK) frequency(t float64) float64 {
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
