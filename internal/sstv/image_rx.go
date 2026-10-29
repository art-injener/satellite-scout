package sstv

import (
	"image"
	"image/color"
	"math"
)

// syncPoint — найденное начало синхроимпульса группы развёртки, номер отсчёта.
type syncPoint struct {
	g   int
	pos float64
}

// imageRx — приём одной картинки после заголовка VIS.
type imageRx struct {
	d    *Decoder
	mode Mode

	nominal float64 // отсчётов в группе развёртки по режиму
	syncLen int     // отсчётов в синхроимпульсе
	window  int     // поиск синхроимпульса: ± отсчётов от ожидаемого места
	a, b    float64 // прямая начал синхроимпульсов: группа g начинается в a + b·g
	points  []syncPoint
	g       int // следующая группа

	y, cb, cr []uint8
	rgba      *image.RGBA
}

func newImageRx(d *Decoder, m Mode, start float64) *imageRx {
	r := &imageRx{
		d:       d,
		mode:    m,
		nominal: m.groupDuration() * d.rate,
		syncLen: max(d.samples(m.sync), 1),
		y:       make([]uint8, m.Width*m.Height),
		cb:      make([]uint8, m.Width*m.Height),
		cr:      make([]uint8, m.Width*m.Height),
		rgba:    image.NewRGBA(image.Rect(0, 0, m.Width, m.Height)),
	}
	r.window = max(r.syncLen*3/2, d.samples(0.005))
	r.a, r.b = start, r.nominal
	for i := range r.cb {
		r.cb[i], r.cr[i] = 128, 128
	}
	return r
}

// advance разворачивает группы, для которых уже есть звук до отсчёта end.
// finished — картинка закончилась; тогда at — отсчёт её конца.
func (r *imageRx) advance(end int) (events []Event, finished bool, at int) {
	groups := r.mode.Groups()
	for r.g < groups {
		pred := r.a + r.b*float64(r.g)
		if float64(end) < pred+r.b+float64(r.window+r.syncLen) {
			break
		}
		r.findSync(r.g, pred)
		r.render(r.g)
		r.g++
		events = append(events[:0], Event{Kind: EventLines, Mode: r.mode, Lines: r.g * r.mode.groupLines(), Image: r.rgba})
	}
	if r.g < groups {
		return events, false, 0
	}
	// Итог: все группы заново по прямой, построенной по всей картинке.
	for g := range groups {
		r.render(g)
	}
	at = int(r.a + r.b*float64(groups))
	return []Event{{Kind: EventDone, Mode: r.mode, Lines: r.mode.Height, Image: r.rgba}}, true, at
}

// findSync ищет синхроимпульс группы g около ожидаемого места pred и уточняет прямую.
func (r *imageRx) findSync(g int, pred float64) {
	d := r.d
	from := int(math.Round(pred)) - r.window
	to := int(math.Round(pred)) + r.window
	lo, hi := from-d.base, to-d.base+r.syncLen
	if lo < 0 || hi > len(d.freq) {
		return
	}
	below := func(i int) int {
		if d.freq[i] < syncBelowHz {
			return 1
		}
		return 0
	}
	count := 0
	for i := lo; i < lo+r.syncLen; i++ {
		count += below(i)
	}
	// При равенстве — позднее место: перед первой группой стоп-бит VIS той же частоты
	// сливается с синхроимпульсом, и синхроимпульс — конец этого плато.
	best, bestAt := count, lo
	for c := lo + 1; c <= to-d.base; c++ {
		count += below(c+r.syncLen-1) - below(c-1)
		if count >= best {
			best, bestAt = count, c
		}
	}
	if float64(best) < syncMinFill*float64(r.syncLen) {
		return
	}
	pos := float64(bestAt + d.base)
	// Когда прямая уже надёжна, далёкая находка — помеха, а не синхроимпульс.
	if len(r.points) >= 4 && math.Abs(pos-pred) > math.Max(2, 0.002*d.rate) {
		return
	}
	r.points = append(r.points, syncPoint{g: g, pos: pos})
	r.fit()
}

// fit — прямая начал синхроимпульсов методом наименьших квадратов; наклон не дальше
// maxClockSkew от номинала.
func (r *imageRx) fit() {
	n := float64(len(r.points))
	if len(r.points) < 2 {
		p := r.points[0]
		r.a = p.pos - r.b*float64(p.g)
		return
	}
	var sg, sp, sgg, sgp float64
	for _, p := range r.points {
		g := float64(p.g)
		sg += g
		sp += p.pos
		sgg += g * g
		sgp += g * p.pos
	}
	den := n*sgg - sg*sg
	if den == 0 {
		return
	}
	b := (n*sgp - sg*sp) / den
	b = math.Min(math.Max(b, r.nominal*(1-maxClockSkew)), r.nominal*(1+maxClockSkew))
	r.b = b
	r.a = (sp - b*sg) / n
}

// render разворачивает группу g по текущей прямой и переводит её строки в RGB.
func (r *imageRx) render(g int) {
	m := r.mode
	w := m.Width
	rate := r.d.rate * r.b / r.nominal // отсчётов в секунде звука передатчика
	c := r.a + r.b*float64(g)
	scanAt := func(off float64) float64 { return c + off*rate }
	rows := func(p []uint8, line int) []uint8 { return p[line*w : (line+1)*w] }

	first := m.sync + m.porch
	switch m.layout {
	case layoutPD:
		l := 2 * g
		scanDur := m.scan * rate
		r.scanRow(rows(r.y, l), scanAt(first), scanDur)
		r.scanRow(rows(r.cr, l), scanAt(first+m.scan), scanDur)
		r.scanRow(rows(r.cb, l), scanAt(first+2*m.scan), scanDur)
		r.scanRow(rows(r.y, l+1), scanAt(first+3*m.scan), scanDur)
		copy(rows(r.cr, l+1), rows(r.cr, l))
		copy(rows(r.cb, l+1), rows(r.cb, l))
		r.toRGB(l)
		r.toRGB(l + 1)
	default:
		r.scanRow(rows(r.y, g), scanAt(first), m.scan*rate)
		chroma := scanAt(first + m.scan + m.sep + m.sepPorch)
		pair := g &^ 1
		plane := r.cr
		if g%2 == 1 {
			plane = r.cb
		}
		r.scanRow(rows(plane, pair), chroma, m.chroma*rate)
		if pair+1 < m.Height {
			copy(rows(plane, pair+1), rows(plane, pair))
		}
		r.toRGB(pair)
		if pair+1 < m.Height {
			r.toRGB(pair + 1)
		}
	}
}

// scanRow заполняет строку значениями пикселей развёртки с отсчёта start длиной dur отсчётов:
// пиксель берётся в середине своего отрезка.
func (r *imageRx) scanRow(dst []uint8, start, dur float64) {
	for i := range dst {
		dst[i] = pixelValue(r.d.at(start + (float64(i)+0.5)*dur/float64(len(dst))))
	}
}

func (r *imageRx) toRGB(line int) {
	w := r.mode.Width
	for x := range w {
		i := line*w + x
		cr, cg, cb := YCbCr{Y: r.y[i], Cb: r.cb[i], Cr: r.cr[i]}.RGB()
		r.rgba.SetRGBA(x, line, color.RGBA{cr, cg, cb, 255})
	}
}
