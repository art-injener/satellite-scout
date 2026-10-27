package imitator

import (
	"bytes"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/modem"
)

type frameSync interface {
	Push(dst [][]byte, levels []byte) [][]byte
	Good() int
	Bad() int
}

// End-to-end reception of the imitator beacon: scene with Doppler and receiver noise,
// Doppler removal and two-stage channel filter, demodulator, HDLC deframer, AX.25 parser.
func TestBeacon_ReceivedThroughDemodChain(t *testing.T) {
	const (
		fs       = 1_024_000.0
		center   = 436_500_000
		offsetHz = 50_000.0
		block    = 1 << 16
		duration = 4 * time.Second
		midRate  = fs / 16
	)
	cases := []struct {
		modem BeaconModem
		// Coarse filter must pass the whole 9600 signal (±8 kHz) flat.
		coarseCut  float64
		coarseTaps int
		fineDecim  int
		fineCut    float64
		demod      func() func([]complex64) []byte
		deframer   frameSync
	}{
		{
			modem: BeaconAFSK1200, coarseCut: 20_000, coarseTaps: 127, fineDecim: 2, fineCut: 7_500,
			demod: func() func([]complex64) []byte {
				d := modem.NewAFSKDemod(modem.AFSK1200(midRate / 2))
				return func(iq []complex64) []byte { return d.Demodulate(nil, iq) }
			},
			deframer: &ax25.Deframer{},
		},
		{
			modem: BeaconFSK9600G3RUH, coarseCut: 25_600, coarseTaps: 257, fineDecim: 1, fineCut: 9_600,
			demod: func() func([]complex64) []byte {
				d := modem.NewFSKDemod(modem.FSK9600(midRate))
				var ds modem.G3RUHDescrambler
				return func(iq []complex64) []byte { return ds.Descramble(nil, d.Demodulate(nil, iq)) }
			},
			deframer: &ax25.Deframer{},
		},
		{
			modem: BeaconFSK9600GEOSCAN, coarseCut: 25_600, coarseTaps: 257, fineDecim: 1, fineCut: 9_600,
			demod: func() func([]complex64) []byte {
				d := modem.NewFSKDemod(modem.FSK9600(midRate))
				return func(iq []complex64) []byte { return d.Demodulate(nil, iq) }
			},
			deframer: &geoscan.Deframer{},
		},
	}
	for _, tc := range cases {
		t.Run(string(tc.modem), func(t *testing.T) {
			beacon := Signal{
				Kind:           SignalBeacon,
				Modem:          tc.modem,
				FrequencyHz:    center + offsetHz,
				LevelDBFS:      -40,
				DopplerHz:      8000,
				DopplerRateHzS: -200,
				Frame:          DefaultBeaconFrame(tc.modem),
				Period:         time.Second,
			}
			// Noise over the whole band fs: SNR ≈ 20 dB in 15 kHz, ≈ 19 dB in 19.2 kHz.
			sc, err := newScene(Options{Signals: []Signal{beacon}, NoiseDBFS: -42, Seed: 1}, fs)
			if err != nil {
				t.Fatalf("newScene: %v", err)
			}

			shift := func(t time.Duration) float64 {
				return offsetHz + beacon.DopplerHz + beacon.DopplerRateHzS*t.Seconds()
			}
			coarse := dsp.NewXlatingFIR(offsetHz, fs, 16,
				dsp.DesignLowpass(tc.coarseCut, fs, tc.coarseTaps, dsp.WindowBlackmanHarris))
			coarse.SetShiftFunc(shift)
			fine := dsp.NewXlatingFIR(0, midRate, tc.fineDecim,
				dsp.DesignLowpass(tc.fineCut, midRate, 255, dsp.WindowBlackmanHarris))
			sq := dsp.Squelch{ThresholdDBFS: -90}
			demod := tc.demod()
			deframer := tc.deframer

			var frames [][]byte
			air := make([]complex64, block)
			for range int(duration.Seconds() * fs / block) {
				sc.fill(air, center)
				ch := fine.Process(coarse.Process(air))
				if sq.Open(ch) {
					frames = deframer.Push(frames, demod(ch))
				}
			}

			// One beacon per second, the last one starts at 3 s and ends before 4 s.
			if len(frames) != 4 {
				t.Errorf("received %d frames in %v, want 4 (good=%d bad=%d)", len(frames), duration, deframer.Good(), deframer.Bad())
			}
			want := DefaultBeaconFrame(tc.modem)
			for i, b := range frames {
				f, err := ax25.Parse(b)
				if err != nil {
					t.Fatalf("frame %d: Parse: %v", i, err)
				}
				// GEOSCAN pads the frame with zeros up to its fixed size.
				if f.Dest != want.Dest || f.Src != want.Src ||
					!bytes.Equal(bytes.TrimRight(f.Info, "\x00"), bytes.TrimRight(want.Info, "\x00")) {
					t.Errorf("frame %d = %s>%s %q, want %s>%s %q", i, f.Src.Call, f.Dest.Call, f.Info, want.Src.Call, want.Dest.Call, want.Info)
				}
			}
		})
	}
}
