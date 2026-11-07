package demod

import (
	"math"
	"math/cmplx"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// shifted moves the signal by hz at the test rate, as Doppler does.
func shifted(iq []complex64, hz float64) []complex64 {
	out := make([]complex64, len(iq))
	for i, s := range iq {
		out[i] = s * complex64(cmplx.Exp(complex(0, 2*math.Pi*hz*float64(i)/testRate)))
	}
	return out
}

// The upper sideband is 2.8 kHz wide: without Doppler correction a 3 kHz shift moves the tone
// out of it, with the correction it sounds where it was.
func TestChain_DopplerKeepsTheChannelOnTheSignal(t *testing.T) {
	const doppler = 3000
	air := shifted(ssbAir(1200, 1700, 1), doppler)
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)
	s := voiceSettings("usb", 2800, -90)

	// Powers at 1200 Hz and where the uncorrected tone would sound.
	tone := func(dopplerHz float64) (float64, float64) {
		c := NewChain()
		c.SetDopplerHz(dopplerHz)
		audio := collectAudio(c, frames, s)
		settled := audio[AudioRate/2:]
		return tonePower(settled, 1200), tonePower(settled, 1200+doppler)
	}
	if want, off := tone(doppler); want < 1000*off {
		t.Errorf("corrected: 1200 Hz power %.2e vs %.0f Hz %.2e, want the tone back at 1200 Hz",
			want, 1200.0+doppler, off)
	}
	if want, _ := tone(0); want > 1e-3 {
		t.Errorf("uncorrected: 1200 Hz power %.2e, want the tone gone from it", want)
	}
}

type dopplerStub struct {
	norad int
	rr    float64
}

func (d dopplerStub) RangeRateKmps(norad int, _ time.Time) (float64, bool) {
	return d.rr, norad == d.norad
}

func TestService_ReportsDopplerOfTheSatellite(t *testing.T) {
	const norad = 25544
	s := voiceSettings("fm", 15_000, -90)
	s.DopplerNorad = norad
	frames := toFrames(fmToneAir(0.3), testRate, sdr.DefaultBlockSize, 0)

	for name, tc := range map[string]struct {
		src  DopplerSource
		want any
	}{
		// Approaching at 5 km/s: the downlink is heard above its nominal frequency.
		"tracked":     {dopplerStub{norad: norad, rr: -5}, tracker.DopplerShiftHz(testCenter+testOffset, -5)},
		"not tracked": {dopplerStub{norad: 1, rr: -5}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			sink := &sinkStub{}
			svc := NewService(&channelsStub{s: s}, sink.emit).WithDoppler(tc.src)
			ch := make(chan sdr.IQFrame, len(frames))
			for _, f := range frames {
				ch <- f
			}
			close(ch)
			svc.Run("radio_path_0", ch)

			st := sink.ofType(EventStatus)
			last := st[len(st)-1]
			if last["doppler_norad"] != float64(norad) {
				t.Errorf("doppler_norad=%v, want %d", last["doppler_norad"], norad)
			}
			got, want := last["doppler_hz"], tc.want
			if want == nil {
				if got != nil {
					t.Errorf("doppler_hz=%v, want none without the orbit", got)
				}
				return
			}
			if g, ok := got.(float64); !ok || math.Abs(g-want.(float64)) > 1 {
				t.Errorf("doppler_hz=%v, want %.0f", got, want)
			}
		})
	}
}
