package dsp_test

import (
	"math"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/modem"
)

// The imitator channel lives in modem, which itself imports dsp: hence an external test.
func TestLevelDBFS_ChannelMatchesImitator(t *testing.T) {
	const (
		fs        = 1_024_000.0
		offset    = 50_000.0
		bandwidth = 15_000.0
		n         = 1 << 19
	)
	channel := func(iq []complex64) []complex64 {
		coarse := dsp.NewXlatingFIR(offset, fs, 16, dsp.DesignLowpass(20_000, fs, 127, dsp.WindowBlackmanHarris))
		fine := dsp.NewXlatingFIR(0, fs/16, 2, dsp.DesignLowpass(bandwidth/2, fs/16, 255, dsp.WindowBlackmanHarris))
		out := fine.Process(coarse.Process(iq))
		return out[255/2:] // transient of both filters
	}

	t.Run("signal", func(t *testing.T) {
		carrier := make([]complex64, n)
		for i := range carrier {
			carrier[i] = complex(float32(math.Pow(10, -40.0/20)), 0)
		}
		ch := modem.NewChannel(modem.ChannelConfig{SampleRate: fs, OffsetHz: constHz(offset)})
		if got := dsp.LevelDBFS(channel(ch.Process(nil, carrier))); math.Abs(float64(got)+40) > 0.1 {
			t.Errorf("channel signal level=%.2f dBFS, want -40", got)
		}
	})

	t.Run("noise", func(t *testing.T) {
		const snrDB = 20.0
		ch := modem.NewChannel(modem.ChannelConfig{
			SampleRate: fs, SNRdB: snrDB, NoiseBandwidthHz: bandwidth, Seed: 3,
		})
		out := channel(ch.Process(nil, make([]complex64, n)))
		// Unit signal has 0 dBFS, so noise in the channel band sits at -SNR.
		if got := dsp.LevelDBFS(out); math.Abs(float64(got)+snrDB) > 0.3 {
			t.Errorf("channel noise level=%.2f dBFS, want %.1f", got, -snrDB)
		}
	})
}

func constHz(hz float64) func(time.Duration) float64 {
	return func(time.Duration) float64 { return hz }
}
