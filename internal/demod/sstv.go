package demod

import (
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

// sstvDeviationHz — девиация, в которую ложится звук ±1; уровень звука декодеру
// не важен, он мерит частоту.
const sstvDeviationHz = 5000

// sstvRx — приём SSTV: звук из ЧМ канала и декодер картинок.
type sstvRx struct {
	fm    *modem.FMAudioDemod
	dec   *sstv.Decoder
	audio []float32
	done  int
}

func newSSTVRx(rate float64) pictureRx {
	return newSSTVRxOpt(rate, true)
}

// newSSTVRxVISOnly — только заголовок VIS: после перестройки или выключения демода
// нельзя сразу схватить ту же передачу по строчным синхро.
func newSSTVRxVISOnly(rate float64) pictureRx {
	return newSSTVRxOpt(rate, false)
}

func newSSTVRxOpt(rate float64, midJoin bool) pictureRx {
	dec := sstv.NewDecoder(AudioRate)
	dec.SetMidJoin(midJoin)
	return &sstvRx{
		fm: modem.NewFMAudioDemod(modem.FMAudioConfig{
			SampleRate: rate, DeviationHz: sstvDeviationHz, AudioRate: AudioRate,
		}),
		dec: dec,
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

// Audio — звук последнего Push: те же тона, что слышит декодер.
func (r *sstvRx) Audio() []float32 { return r.audio }
