package demod

import (
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

const (
	// sstvAudioRate — частота звука для декодера SSTV: тона до 2,3 кГц с запасом.
	sstvAudioRate = 12_000
	// sstvDeviationHz — девиация, в которую ложится звук ±1; уровень звука декодеру
	// не важен, он мерит частоту.
	sstvDeviationHz = 5000
)

// sstvRx — приём SSTV: звук из ЧМ канала и декодер картинок.
type sstvRx struct {
	fm    *modem.FMAudioDemod
	dec   *sstv.Decoder
	audio []float32
	done  int
}

func newSSTVRx(rate float64) pictureRx {
	return &sstvRx{
		fm: modem.NewFMAudioDemod(modem.FMAudioConfig{
			SampleRate: rate, DeviationHz: sstvDeviationHz, AudioRate: sstvAudioRate,
		}),
		dec: sstv.NewDecoder(sstvAudioRate),
	}
}

func (r *sstvRx) Push(iq []complex64) []sstv.Event {
	r.audio = r.fm.Demodulate(r.audio[:0], iq)
	events := r.dec.Push(r.audio)
	for _, e := range events {
		if e.Kind == sstv.EventDone {
			r.done++
		}
	}
	return events
}

// Done — сколько картинок принято до конца.
func (r *sstvRx) Done() int { return r.done }
