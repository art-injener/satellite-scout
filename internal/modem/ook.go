package modem

import (
	"math"
	"strings"
)

// ookRampFraction — доля символа на фронт: прямоугольная манипуляция дала бы щелчки
// и боковые лепестки на десятки килогерц.
const ookRampFraction = 0.1

// OOKConfig — амплитудная манипуляция «есть/нет» (OOK, она же телеграф CW).
type OOKConfig struct {
	SampleRate float64 // частота дискретизации IQ, Гц
	Baud       float64 // символьная скорость, Бод; у CW — точек в секунду (CWBaud)
}

// OOK — модулятор: уровень 1 — несущая, 0 — тишина, фронты по приподнятому косинусу.
// Состояние сохраняется между вызовами Modulate.
type OOK struct {
	cfg     OOKConfig
	ramp    int
	prev    float64 // амплитуда предыдущего символа
	symbols int
	emitted int
}

// NewOOK создаёт модулятор.
func NewOOK(cfg OOKConfig) *OOK {
	ramp := max(int(math.Round(ookRampFraction*cfg.SampleRate/cfg.Baud)), 1)
	return &OOK{cfg: cfg, ramp: ramp}
}

// Modulate дописывает в dst отсчёты для уровней линии (0 или 1) и возвращает расширенный срез.
func (m *OOK) Modulate(dst []complex64, levels []byte) []complex64 {
	samplesPerSymbol := m.cfg.SampleRate / m.cfg.Baud
	for _, level := range levels {
		a := 0.0
		if level != 0 {
			a = 1
		}
		m.symbols++
		end := int(math.Round(float64(m.symbols) * samplesPerSymbol))
		for k := 0; m.emitted < end; k++ {
			v := a
			if k < m.ramp && a != m.prev {
				w := 0.5 * (1 - math.Cos(math.Pi*float64(k)/float64(m.ramp)))
				v = m.prev + (a-m.prev)*w
			}
			dst = append(dst, complex(float32(v), 0))
			m.emitted++
		}
		m.prev = a
	}
	return dst
}

// CWBaud — точек в секунду при скорости wpm слов в минуту: эталонное слово PARIS — 50 точек.
func CWBaud(wpm float64) float64 { return wpm * 50 / 60 }

// morseCode — точки и тире знаков.
var morseCode = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..", 'E': ".", 'F': "..-.", 'G': "--.",
	'H': "....", 'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..", 'M': "--", 'N': "-.",
	'O': "---", 'P': ".--.", 'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-", 'U': "..-",
	'V': "...-", 'W': ".--", 'X': "-..-", 'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--", '4': "....-",
	'5': ".....", '6': "-....", '7': "--...", '8': "---..", '9': "----.", '/': "-..-.",
}

// MorseLevels — уровни ключа по точкам: точка — 1, тире — 3, пауза внутри знака — 1,
// между знаками — 3, между словами — 7. Незнакомые символы пропускаются.
func MorseLevels(text string) []byte {
	var out []byte
	gap := 3 // пауза перед следующим знаком, в точках
	for _, r := range strings.ToUpper(text) {
		code, ok := morseCode[r]
		switch {
		case r == ' ':
			gap = 7
		case ok:
			if len(out) > 0 {
				out = appendLevel(out, 0, gap)
			}
			out = appendMorse(out, code)
			gap = 3
		}
	}
	return out
}

// appendMorse дописывает элементы знака с паузой в точку между ними.
func appendMorse(out []byte, code string) []byte {
	for i, e := range code {
		if i > 0 {
			out = append(out, 0)
		}
		n := 1
		if e == '-' {
			n = 3
		}
		out = appendLevel(out, 1, n)
	}
	return out
}

func appendLevel(out []byte, level byte, n int) []byte {
	for range n {
		out = append(out, level)
	}
	return out
}
