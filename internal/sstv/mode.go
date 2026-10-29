// Package sstv — передача картинок медленным телевидением (SSTV): режимы Robot 36,
// PD120 и PD180, кодер картинки в звук для имитатора и декодер звука в картинку.
// Тайминги и частоты — по описанию режимов JL Barber, «Proposal for SSTV Mode
// Specifications» (Dayton, 2000); чужой код как образец не используется (ADR-005).
package sstv

import "time"

// Частоты звука, Гц.
const (
	SyncHz    = 1200 // синхроимпульс, старт и стоп VIS
	BlackHz   = 1500 // яркость 0
	WhiteHz   = 2300 // яркость 255
	LeaderHz  = 1900 // ведущий тон заголовка VIS
	VISOneHz  = 1100 // бит VIS 1
	VISZeroHz = 1300 // бит VIS 0
)

// Длительности заголовка VIS, с.
const (
	visLeader = 0.300
	visBreak  = 0.010
	visBit    = 0.030
	// visDuration — ведущий, разрыв, ведущий, старт, 7 бит кода, чётность, стоп.
	visDuration = 2*visLeader + visBreak + 10*visBit
)

// layout — как строки картинки идут в звуке.
type layout int

const (
	// layoutRobot — Robot 36: строка — яркость, разделитель и одна цветность через строку:
	// чётные строки несут R−Y (разделитель 1500 Гц), нечётные — B−Y (2300 Гц).
	layoutRobot layout = iota
	// layoutPD — PD: пара строк — синхро, яркость первой, R−Y и B−Y пары, яркость второй.
	layoutPD
)

// Mode — режим SSTV.
type Mode struct {
	ID            string // имя в конфиге и профилях демодулятора
	Name          string
	VIS           byte
	Width, Height int

	layout layout
	sync   float64 // синхроимпульс, с
	porch  float64 // площадка чёрного после синхро, с
	scan   float64 // развёртка яркости строки, с
	// Только Robot 36: разделитель перед цветностью, площадка после него и развёртка цветности.
	sep, sepPorch, chroma float64
}

var (
	// Robot36 — 320×240, 150 мс на строку, цветность через строку.
	Robot36 = Mode{ID: "robot36", Name: "Robot 36", VIS: 8, Width: 320, Height: 240, layout: layoutRobot,
		sync: 0.009, porch: 0.003, scan: 0.088, sep: 0.0045, sepPorch: 0.0015, chroma: 0.044}
	// PD120 — 640×496, пиксель 0,19 мс.
	PD120 = Mode{ID: "pd120", Name: "PD120", VIS: 95, Width: 640, Height: 496, layout: layoutPD,
		sync: 0.020, porch: 0.00208, scan: 640 * 0.00019}
	// PD180 — 640×496, пиксель 0,286 мс.
	PD180 = Mode{ID: "pd180", Name: "PD180", VIS: 96, Width: 640, Height: 496, layout: layoutPD,
		sync: 0.020, porch: 0.00208, scan: 640 * 0.000286}
)

// Modes — поддержанные режимы.
var Modes = []Mode{Robot36, PD120, PD180}

// ModeByVIS — режим по коду VIS.
func ModeByVIS(vis byte) (Mode, bool) {
	for _, m := range Modes {
		if m.VIS == vis {
			return m, true
		}
	}
	return Mode{}, false
}

// ModeByID — режим по имени в конфиге.
func ModeByID(id string) (Mode, bool) {
	for _, m := range Modes {
		if m.ID == id {
			return m, true
		}
	}
	return Mode{}, false
}

// groupLines — строк картинки в одной группе развёртки.
func (m Mode) groupLines() int {
	if m.layout == layoutPD {
		return 2
	}
	return 1
}

// groupDuration — длительность группы развёртки, с.
func (m Mode) groupDuration() float64 {
	if m.layout == layoutPD {
		return m.sync + m.porch + 4*m.scan
	}
	return m.sync + m.porch + m.scan + m.sep + m.sepPorch + m.chroma
}

// Groups — групп развёртки в картинке.
func (m Mode) Groups() int {
	return m.Height / m.groupLines()
}

// Duration — длительность передачи с заголовком VIS.
func (m Mode) Duration() time.Duration {
	s := visDuration + float64(m.Groups())*m.groupDuration()
	return time.Duration(s * float64(time.Second))
}

// PixelHz — частота яркости или цветности v.
func PixelHz(v uint8) float64 {
	return BlackHz + float64(v)*(WhiteHz-BlackHz)/255
}

// YCbCr — пиксель в BT.601 с ограниченным размахом (Y 16…235, Cb и Cr 16…240):
// так его передают SSTV-программы.
type YCbCr struct{ Y, Cb, Cr uint8 }

// YCbCrFromRGB переводит пиксель RGB в YCbCr.
func YCbCrFromRGB(r, g, b uint8) YCbCr {
	fr, fg, fb := float64(r)/255, float64(g)/255, float64(b)/255
	return YCbCr{
		Y:  clamp8(16 + 65.481*fr + 128.553*fg + 24.966*fb),
		Cb: clamp8(128 - 37.797*fr - 74.203*fg + 112.0*fb),
		Cr: clamp8(128 + 112.0*fr - 93.786*fg - 18.214*fb),
	}
}

// RGB переводит пиксель обратно в RGB.
func (c YCbCr) RGB() (r, g, b uint8) {
	y := 1.164383 * (float64(c.Y) - 16)
	cb, cr := float64(c.Cb)-128, float64(c.Cr)-128
	return clamp8(y + 1.596027*cr), clamp8(y - 0.391762*cb - 0.812968*cr), clamp8(y + 2.017232*cb)
}

func clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	}
	return uint8(v + 0.5)
}
