package imitator

import (
	"fmt"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

// SSTVDeviationHz — девиация ЧМ передатчика SSTV при звуке ±1, как у голосовых
// ЧМ-передатчиков КА.
const SSTVDeviationHz = 5000

// audioSource — звук передачи с частотой IQ; Read возвращает меньше len(dst) в конце.
type audioSource interface {
	Read(dst []float32) int
}

// sstvGen — передача картинки SSTV (своей или записанной) в ЧМ раз в Period,
// между передачами тишина.
type sstvGen struct {
	newAudio      func() audioSource
	sampleRate    float64
	periodSamples int

	enc         audioSource
	fm          *modem.FM
	audio       []float32
	sent        int // отсчётов текущей передачи выдано
	silenceLeft int
}

func newSSTVGen(sig Signal, sampleRate float64) (*sstvGen, error) {
	g := &sstvGen{sampleRate: sampleRate, periodSamples: int(sig.Period.Seconds() * sampleRate)}
	switch {
	case len(sig.Audio) > 0:
		if sig.AudioRate <= 0 {
			return nil, fmt.Errorf("%w: SSTV recording without a sample rate", ErrInvalidSignal)
		}
		rec := normalized(sig.Audio)
		g.newAudio = func() audioSource { return &resampled{src: rec, step: sig.AudioRate / sampleRate} }
	case sig.SSTVMode.Width == 0:
		return nil, fmt.Errorf("%w: SSTV without a mode", ErrInvalidSignal)
	default:
		img := sig.Image
		if img == nil {
			img = sstv.TestPattern(sig.SSTVMode.Width, sig.SSTVMode.Height)
		}
		g.newAudio = func() audioSource { return sstv.NewEncoder(img, sig.SSTVMode, sampleRate) }
	}
	g.start()
	return g, nil
}

func (g *sstvGen) start() {
	g.enc = g.newAudio()
	g.fm = modem.NewFM(modem.FMConfig{SampleRate: g.sampleRate, DeviationHz: SSTVDeviationHz})
	g.sent = 0
}

func (g *sstvGen) fillClean(dst []complex64) {
	i := 0
	for i < len(dst) {
		if g.enc == nil {
			n := min(g.silenceLeft, len(dst)-i)
			clear(dst[i : i+n])
			i += n
			g.silenceLeft -= n
			if g.silenceLeft == 0 {
				g.start()
			}
			continue
		}
		if cap(g.audio) < len(dst)-i {
			g.audio = make([]float32, len(dst)-i)
		}
		audio := g.audio[:len(dst)-i]
		n := g.enc.Read(audio)
		g.fm.Modulate(dst[i:i], audio[:n])
		i += n
		g.sent += n
		if n < len(audio) {
			g.enc = nil
			g.silenceLeft = max(g.periodSamples-g.sent, 0)
			if g.silenceLeft == 0 {
				g.start()
			}
		}
	}
}

// normalized — копия звука с пиком 1: запись любого уровня занимает полную девиацию.
func normalized(audio []float32) []float32 {
	var peak float32
	for _, a := range audio {
		peak = max(peak, a, -a)
	}
	out := make([]float32, len(audio))
	if peak == 0 {
		return out
	}
	for i, a := range audio {
		out[i] = a / peak
	}
	return out
}

// resampled читает запись с шагом step отсчётов записи на отсчёт IQ, линейной интерполяцией.
type resampled struct {
	src  []float32
	step float64
	pos  float64
}

func (r *resampled) Read(dst []float32) int {
	last := float64(len(r.src) - 1)
	for i := range dst {
		if r.pos > last {
			return i
		}
		j := int(r.pos)
		frac := float32(r.pos - float64(j))
		v := r.src[j]
		if j+1 < len(r.src) {
			v += frac * (r.src[j+1] - v)
		}
		dst[i] = v
		r.pos += r.step
	}
	return len(dst)
}
