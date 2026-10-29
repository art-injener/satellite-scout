package sstv

import (
	"image"
	"math"
)

// segment — отрезок звука: тон или развёртка строки пикселей.
type segment struct {
	start, dur float64 // с от начала передачи
	hz         float64 // частота тона; для развёртки не используется
	row        []uint8 // значения пикселей развёртки; nil — тон
}

// Encoder переводит картинку в звук SSTV с непрерывной фазой: заголовок VIS и строки
// по режиму. Звук выдаётся по частям через Read, поэтому длинная передача не держится
// в памяти целиком. Не потокобезопасен.
type Encoder struct {
	mode     Mode
	rate     float64
	y        []uint8 // яркость картинки, приведённой к размеру режима
	segments []segment
	seg      int
	n        int // отсчётов выдано
	phase    float64
}

// NewEncoder создаёт кодер; картинка приводится к размеру режима выбором ближайшего пикселя.
func NewEncoder(img image.Image, m Mode, sampleRate float64) *Encoder {
	e := &Encoder{mode: m, rate: sampleRate}
	y, cb, cr := planes(img, m.Width, m.Height)
	e.y = y
	e.segments = e.build(cb, cr)
	return e
}

// planes — яркость и цветность картинки размером w×h по строкам.
func planes(img image.Image, w, h int) (y, cb, cr []uint8) {
	b := img.Bounds()
	y, cb, cr = make([]uint8, w*h), make([]uint8, w*h), make([]uint8, w*h)
	for j := range h {
		sy := b.Min.Y + j*b.Dy()/h
		for i := range w {
			sx := b.Min.X + i*b.Dx()/w
			r, g, bl, _ := img.At(sx, sy).RGBA()
			c := YCbCrFromRGB(uint8(r>>8), uint8(g>>8), uint8(bl>>8))
			y[j*w+i], cb[j*w+i], cr[j*w+i] = c.Y, c.Cb, c.Cr
		}
	}
	return y, cb, cr
}

// pairMean — средняя по паре строк line и line+1 строка плоскости.
func pairMean(p []uint8, w, line int) []uint8 {
	a, b := p[line*w:(line+1)*w], p[(line+1)*w:(line+2)*w]
	out := make([]uint8, w)
	for i := range out {
		out[i] = uint8((int(a[i]) + int(b[i]) + 1) / 2)
	}
	return out
}

func (e *Encoder) build(cb, cr []uint8) []segment {
	m := e.mode
	w := m.Width
	var segs []segment
	t := 0.0
	tone := func(hz, dur float64) {
		segs = append(segs, segment{start: t, dur: dur, hz: hz})
		t += dur
	}
	scan := func(row []uint8, dur float64) {
		segs = append(segs, segment{start: t, dur: dur, row: row})
		t += dur
	}

	tone(LeaderHz, visLeader)
	tone(SyncHz, visBreak)
	tone(LeaderHz, visLeader)
	tone(SyncHz, visBit)
	ones := 0
	for i := range 7 {
		bit := m.VIS >> i & 1
		ones += int(bit)
		tone(visBitHz(bit), visBit)
	}
	tone(visBitHz(byte(ones&1)), visBit)
	tone(SyncHz, visBit)

	for g := range m.Groups() {
		tone(SyncHz, m.sync)
		tone(BlackHz, m.porch)
		switch m.layout {
		case layoutPD:
			l := 2 * g
			scan(e.y[l*w:(l+1)*w], m.scan)
			scan(pairMean(cr, w, l), m.scan)
			scan(pairMean(cb, w, l), m.scan)
			scan(e.y[(l+1)*w:(l+2)*w], m.scan)
		default:
			scan(e.y[g*w:(g+1)*w], m.scan)
			pair := g &^ 1
			if g%2 == 0 {
				tone(BlackHz, m.sep)
				tone(LeaderHz, m.sepPorch)
				scan(pairMean(cr, w, pair), m.chroma)
			} else {
				tone(WhiteHz, m.sep)
				tone(LeaderHz, m.sepPorch)
				scan(pairMean(cb, w, pair), m.chroma)
			}
		}
	}
	return segs
}

func visBitHz(bit byte) float64 {
	if bit == 1 {
		return VISOneHz
	}
	return VISZeroHz
}

// Read заполняет dst звуком амплитуды 1 и возвращает число отсчётов; меньше len(dst) —
// передача закончилась.
func (e *Encoder) Read(dst []float32) int {
	for i := range dst {
		t := float64(e.n) / e.rate
		for e.seg < len(e.segments) && t >= e.segments[e.seg].start+e.segments[e.seg].dur {
			e.seg++
		}
		if e.seg == len(e.segments) {
			return i
		}
		e.phase = math.Mod(e.phase+2*math.Pi*e.segments[e.seg].freqAt(t)/e.rate, 2*math.Pi)
		dst[i] = float32(math.Sin(e.phase))
		e.n++
	}
	return len(dst)
}

// freqAt — частота отрезка в момент t.
func (s *segment) freqAt(t float64) float64 {
	if s.row == nil {
		return s.hz
	}
	px := min(int((t-s.start)/s.dur*float64(len(s.row))), len(s.row)-1)
	return PixelHz(s.row[px])
}
