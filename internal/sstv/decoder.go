package sstv

import (
	"image"
	"math"
	"math/cmplx"

	"github.com/art-injener/satellite-scout/internal/dsp"
)

const (
	// mixHz — середина полосы SSTV: звук переносится к нулю, чтобы взять мгновенную частоту.
	mixHz = 1900
	// freqCutoffHz — полоса оценки частоты вокруг mixHz: 1100…2300 Гц помещается,
	// зеркальная составляющая на 2·mixHz подавлена. Шире не нужно: на записях из эфира
	// шум в широкой полосе портит картинку сильнее, чем размытие краёв развёртки.
	freqCutoffHz = 1500
	// dcCutoffHz — срез фильтра постоянной составляющей на входе. Несущая не в центре
	// канала даёт после дискриминатора постоянную в звуке, а после переноса на mixHz —
	// помеху посреди полосы SSTV. Тона от 1100 Гц срез не задевает.
	dcCutoffHz = 100
	// visCheckEvery — шаг поиска заголовка VIS, с.
	visCheckEvery = 0.001
	// searchKeep — сколько звука держать при поиске заголовка, с.
	searchKeep = 1.2
	// syncBelowHz — частота ниже этой считается синхроимпульсом (1200 Гц против чёрного 1500).
	syncBelowHz = 1350
	// syncMinFill — доля отсчётов окна синхроимпульса ниже syncBelowHz, чтобы его принять.
	syncMinFill = 0.6
	// maxClockSkew — допустимое расхождение тактов передатчика и приёмника.
	maxClockSkew = 0.02
)

// EventKind — вид события декодера.
type EventKind int

const (
	// EventStart — принят заголовок VIS, началась картинка.
	EventStart EventKind = iota
	// EventLines — проявились новые строки.
	EventLines
	// EventDone — картинка закончилась; Image перерисована с итоговой поправкой наклона.
	EventDone
)

// Event — событие декодера. Image принадлежит декодеру до следующего EventStart.
type Event struct {
	Kind  EventKind
	Mode  Mode
	Lines int // строк готово
	Image *image.RGBA
}

// Decoder — приёмник SSTV из звука: мгновенная частота, поиск заголовка VIS, поиск
// синхроимпульсов строк и прямая по ним — она снимает наклон от расхождения тактов,
// затем развёртка пикселей и перевод YCbCr в RGB. Строки проявляются по ходу приёма,
// в конце картинка перерисовывается по итоговой прямой. Не потокобезопасен.
type Decoder struct {
	rate float64

	dcPole, dcX, dcY float64 // фильтр постоянной составляющей

	mixPhase, mixInc float64
	taps             []float64
	hist             []complex128 // история ФНЧ дважды подряд
	hp               int
	prevZ            complex128

	freq      []float32 // мгновенная частота, Гц; freq[0] — отсчёт номер base
	base      int
	nextCheck int // номер отсчёта следующей проверки заголовка

	img *imageRx // принимаемая картинка; nil — поиск заголовка
}

// NewDecoder создаёт декодер звука с частотой отсчётов audioRate.
func NewDecoder(audioRate float64) *Decoder {
	n := int(math.Ceil(4*audioRate/freqCutoffHz)) | 1
	d := &Decoder{
		rate:   audioRate,
		dcPole: math.Exp(-2 * math.Pi * dcCutoffHz / audioRate),
		mixInc: 2 * math.Pi * mixHz / audioRate,
		taps:   dsp.DesignLowpass(freqCutoffHz, audioRate, n, dsp.WindowBlackmanHarris),
	}
	d.hist = make([]complex128, 2*len(d.taps))
	return d
}

// Push принимает звук и возвращает события, случившиеся в нём.
func (d *Decoder) Push(audio []float32) []Event {
	var events []Event
	for _, a := range audio {
		d.freq = append(d.freq, d.instantHz(float64(a)))
	}
	end := d.base + len(d.freq)
	for {
		if d.img == nil {
			ev, ok := d.searchVIS(end)
			if !ok {
				break
			}
			events = append(events, ev)
			continue
		}
		ev, finished, at := d.img.advance(end)
		events = append(events, ev...)
		if !finished {
			break
		}
		d.img = nil
		d.nextCheck = at
	}
	if d.img == nil {
		d.trim(end - int(searchKeep*d.rate))
	}
	return events
}

// instantHz — мгновенная частота звука: без постоянной составляющей, перенос mixHz к нулю,
// ФНЧ, поворот фазы.
func (d *Decoder) instantHz(in float64) float32 {
	a := in - d.dcX + d.dcPole*d.dcY
	d.dcX, d.dcY = in, a
	sin, cos := math.Sincos(d.mixPhase)
	d.mixPhase = math.Mod(d.mixPhase+d.mixInc, 2*math.Pi)
	x := complex(a*cos, -a*sin)
	n := len(d.taps)
	d.hist[d.hp], d.hist[d.hp+n] = x, x
	d.hp = (d.hp + 1) % n
	var z complex128
	for k, t := range d.taps {
		z += complex(t, 0) * d.hist[d.hp+k]
	}
	f := mixHz + cmplx.Phase(z*cmplx.Conj(d.prevZ))*d.rate/(2*math.Pi)
	d.prevZ = z
	return float32(f)
}

// trim отбрасывает звук до отсчёта from.
func (d *Decoder) trim(from int) {
	if from <= d.base {
		return
	}
	n := min(from-d.base, len(d.freq))
	d.freq = append(d.freq[:0], d.freq[n:]...)
	d.base += n
}

func (d *Decoder) samples(sec float64) int { return int(math.Round(sec * d.rate)) }

// fill — доля отсчётов [from, to) с частотой в [lo, hi] и их средняя частота.
func (d *Decoder) fill(from, to int, lo, hi float32) (frac, mean float64) {
	from, to = max(from-d.base, 0), min(to-d.base, len(d.freq))
	if to <= from {
		return 0, 0
	}
	in, sum := 0, 0.0
	for _, f := range d.freq[from:to] {
		if f >= lo && f <= hi {
			in++
		}
		sum += float64(f)
	}
	return float64(in) / float64(to-from), sum / float64(to-from)
}

// searchVIS проверяет, не закончился ли к отсчёту end заголовок VIS, с шагом visCheckEvery.
func (d *Decoder) searchVIS(end int) (Event, bool) {
	step := max(d.samples(visCheckEvery), 1)
	tail := d.samples(10 * visBit) // старт, 8 бит и стоп
	for ; d.nextCheck <= end; d.nextCheck += step {
		s := d.nextCheck - tail // начало стартового бита
		if s-d.samples(visLeader) < d.base {
			continue
		}
		m, ok := d.matchVIS(s)
		if !ok {
			continue
		}
		start := float64(d.refineEdge(s) + tail)
		d.img = newImageRx(d, m, start)
		d.nextCheck = end
		return Event{Kind: EventStart, Mode: m, Image: d.img.rgba}, true
	}
	return Event{}, false
}

// matchVIS — есть ли заголовок VIS со стартовым битом с отсчёта s и какой в нём режим.
func (d *Decoder) matchVIS(s int) (Mode, bool) {
	bit := d.samples(visBit)
	guard := d.samples(0.005)
	if f, _ := d.fill(s-d.samples(visLeader-0.05), s-guard, LeaderHz-100, LeaderHz+100); f < 0.6 {
		return Mode{}, false
	}
	if f, _ := d.fill(s+guard, s+bit-guard, SyncHz-70, SyncHz+70); f < 0.6 {
		return Mode{}, false
	}
	var code, ones byte
	for k := range 8 {
		from := s + (k+1)*bit
		f, mean := d.fill(from+guard, from+bit-guard, VISOneHz-70, VISZeroHz+70)
		if f < 0.6 || math.Abs(mean-SyncHz) < 40 {
			return Mode{}, false
		}
		if mean < SyncHz {
			ones++
			if k < 7 {
				code |= 1 << k
			}
		}
	}
	stop := s + 9*bit
	if f, _ := d.fill(stop+guard, stop+bit-guard, SyncHz-70, SyncHz+70); f < 0.6 || ones%2 != 0 {
		return Mode{}, false
	}
	return ModeByVIS(code)
}

// refineEdge уточняет начало стартового бита по переходу с ведущего тона 1900 Гц на 1200 Гц.
func (d *Decoder) refineEdge(s int) int {
	span := d.samples(0.015)
	mid := float32(LeaderHz+SyncHz) / 2
	for i := max(s-span, d.base+1); i < s+span && i-d.base < len(d.freq); i++ {
		if d.freq[i-d.base-1] >= mid && d.freq[i-d.base] < mid {
			return i
		}
	}
	return s
}

// at — мгновенная частота в дробный отсчёт pos линейной интерполяцией.
func (d *Decoder) at(pos float64) float64 {
	i := int(math.Floor(pos)) - d.base
	if i < 0 {
		return float64(d.freq[0])
	}
	if i >= len(d.freq)-1 {
		return float64(d.freq[len(d.freq)-1])
	}
	fr := pos - math.Floor(pos)
	return float64(d.freq[i])*(1-fr) + float64(d.freq[i+1])*fr
}

// pixelValue — яркость или цветность по частоте.
func pixelValue(hz float64) uint8 {
	return clamp8((hz - BlackHz) * 255 / (WhiteHz - BlackHz))
}
