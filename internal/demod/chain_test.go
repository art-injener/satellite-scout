package demod

import (
	"bytes"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	testRate   = 1_024_000
	testCenter = 436_500_000
	testOffset = 50_000
	testLevel  = -40.0 // dBFS of the beacon at the receiver input
	testSNR    = 20.0  // dB in the 15 kHz channel: noise in the channel at -60 dBFS
)

func testFrame() ax25.Frame {
	return ax25.Frame{Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS0SIM"}, Info: []byte("chain test beacon")}
}

func afskSettings(offsetHz int64, squelchDBFS float64) sdr.ChannelSettings {
	return sdr.ChannelSettings{OffsetHz: offsetHz, BandwidthHz: 15_000, SquelchDBFS: squelchDBFS, Demod: "afsk1200"}
}

func fsk9600Settings(offsetHz int64, squelchDBFS float64) sdr.ChannelSettings {
	return sdr.ChannelSettings{OffsetHz: offsetHz, BandwidthHz: sdr.MaxChannelBandwidthHz, SquelchDBFS: squelchDBFS, Demod: "fsk9600_g3ruh"}
}

// The widest channel the server accepts must lie in the flat part of the coarse filter
// at every sample rate a receiver offers, so the channel edges drawn on the spectrum are real.
func TestChain_WidestChannelIsFlat(t *testing.T) {
	for _, rate := range []float64{250_000, 300_000, 1_024_000, 2_048_000, 2_400_000, 2_500_000, 3_000_000, 6_000_000, 10_000_000} {
		if flat := 0.3 * midRate(rate); flat < sdr.MaxChannelBandwidthHz {
			t.Errorf("rate %.0f: flat band %.0f Hz, narrower than the widest channel %d Hz", rate, flat, sdr.MaxChannelBandwidthHz)
		}
	}
}

// burstLevels — line levels of one beacon frame, as the imitator transmits it.
func burstLevels(t *testing.T) []byte {
	t.Helper()
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return ax25.NRZI(ax25.HDLCBits(b, 32, 2))
}

func burstSamples(t *testing.T, rate float64) int {
	return int(math.Round(float64(len(burstLevels(t))) * rate / 1200))
}

// geoscanData — the test frame as GEOSCAN carries it: AX.25 without FCS, zero-padded to the long size.
func geoscanData(t *testing.T) []byte {
	t.Helper()
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, geoscan.LongDataLen)
	copy(data, b[:len(b)-2])
	return data
}

// modulateBurst — one beacon frame in AFSK 1200, FSK 9600 G3RUH or GEOSCAN at the given rate.
func modulateBurst(t *testing.T, rate float64, demod string) []complex64 {
	t.Helper()
	switch demod {
	case "fsk9600_g3ruh":
		var s modem.G3RUHScrambler
		return modem.NewFSK(modem.FSK9600(rate)).Modulate(nil, s.Scramble(nil, burstLevels(t)))
	case "fsk9600_geoscan":
		levels, err := geoscan.Encode(geoscanData(t), 32, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.FSK9600(rate)).Modulate(nil, levels)
	}
	return modem.NewAFSK(modem.AFSK1200(rate)).Modulate(nil, burstLevels(t))
}

// beaconAir — receiver input: an AFSK 1200 beacon every period starting at 0, offset from
// the tuning, with noise at testSNR in 15 kHz, scaled to testLevel.
func beaconAir(t *testing.T, rate float64, bursts int, period time.Duration) []complex64 {
	t.Helper()
	return beaconAirOf(t, rate, bursts, period, "afsk1200")
}

func beaconAirOf(t *testing.T, rate float64, bursts int, period time.Duration, demod string) []complex64 {
	t.Helper()
	clean := make([]complex64, int(float64(bursts)*period.Seconds()*rate))
	burst := modulateBurst(t, rate, demod)
	for k := range bursts {
		copy(clean[int(float64(k)*period.Seconds()*rate):], burst)
	}
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate:       rate,
		OffsetHz:         func(time.Duration) float64 { return testOffset },
		SNRdB:            testSNR,
		NoiseBandwidthHz: 15_000,
		Seed:             7,
	})
	air := ch.Process(nil, clean)
	gain := complex(float32(math.Pow(10, testLevel/20)), 0)
	for i := range air {
		air[i] *= gain
	}
	return air
}

func toFrames(air []complex64, rate uint32, block int, startMs uint64) []sdr.IQFrame {
	var out []sdr.IQFrame
	for i := 0; i < len(air); i += block {
		out = append(out, sdr.IQFrame{
			Samples:     air[i:min(i+block, len(air))],
			CenterHz:    testCenter,
			SampleRate:  rate,
			TimestampMs: startMs + uint64(i)*1000/uint64(rate),
		})
	}
	return out
}

func run(c *Chain, frames []sdr.IQFrame, s sdr.ChannelSettings) []Packet {
	var out []Packet
	for _, f := range frames {
		out = append(out, c.Process(f, s)...)
	}
	return out
}

func checkPackets(t *testing.T, got []Packet, want int) {
	t.Helper()
	if len(got) != want {
		t.Fatalf("packets=%d, want %d", len(got), want)
	}
	for i, p := range got {
		f := testFrame()
		if p.Frame.Src != f.Src || p.Frame.Dest != f.Dest || !bytes.Equal(p.Frame.Info, f.Info) {
			t.Errorf("packet %d frame=%+v, want %+v", i, p.Frame, f)
		}
	}
}

func TestChain_DecodesBeacons(t *testing.T) {
	c := NewChain()
	frames := toFrames(beaconAir(t, testRate, 3, time.Second), testRate, sdr.DefaultBlockSize, 1_000_000)
	got := run(c, frames, afskSettings(testOffset, -90))

	checkPackets(t, got, 3)
	for i, p := range got {
		if p.FrequencyHz != testCenter+testOffset {
			t.Errorf("packet %d frequency=%d, want %d", i, p.FrequencyHz, testCenter+testOffset)
		}
		if p.TimestampMs < 1_000_000 || p.TimestampMs > 1_003_000 {
			t.Errorf("packet %d timestamp=%d, want within the stream", i, p.TimestampMs)
		}
	}
	if b, err := testFrame().Bytes(); err == nil && !bytes.Equal(got[0].Raw, b[:len(b)-2]) {
		t.Errorf("raw=% X, want frame bytes without FCS", got[0].Raw)
	}
	if st := c.Status(); st.Good != 3 || !st.Supported {
		t.Errorf("status=%+v, want 3 good, supported", st)
	}
}

func TestChain_SquelchHoldKeepsFrameTail(t *testing.T) {
	// The last block starts mid closing flag (12 bits before the burst end, 2 postamble flags):
	// the frame completes there, yet ~10 ms of signal in ~150 ms keeps that block at about -50.5 dBFS.
	tail := 12 * testRate / 1200
	block := (burstSamples(t, testRate) - tail) / 3
	air := beaconAir(t, testRate, 1, time.Second)
	got := run(NewChain(), toFrames(air, testRate, block, 0), afskSettings(testOffset, testLevel-8))
	checkPackets(t, got, 1)
}

func TestChain_SquelchAboveSignalReceivesNothing(t *testing.T) {
	c := NewChain()
	frames := toFrames(beaconAir(t, testRate, 2, time.Second), testRate, sdr.DefaultBlockSize, 0)
	if got := run(c, frames, afskSettings(testOffset, testLevel+20)); len(got) != 0 {
		t.Fatalf("packets=%d, want 0 with squelch above the signal", len(got))
	}
	st := c.Status()
	if st.SquelchOpen {
		t.Error("squelch reported open")
	}
	// The last block is noise only: about -60 dBFS in the channel.
	if st.LevelDBFS > -55 || st.LevelDBFS < -65 {
		t.Errorf("level=%.1f dBFS, want about -60", st.LevelDBFS)
	}
}

func TestChain_FollowsOffsetChange(t *testing.T) {
	c := NewChain()
	frames := toFrames(beaconAir(t, testRate, 3, time.Second), testRate, sdr.DefaultBlockSize, 0)
	var got []Packet
	for i, f := range frames {
		// Channel off the beacon for the first burst, on it from 0.9 s.
		s := afskSettings(0, -90)
		if i*sdr.DefaultBlockSize >= testRate*9/10 {
			s = afskSettings(testOffset, -90)
		}
		got = append(got, c.Process(f, s)...)
	}
	checkPackets(t, got, 2)
}

func TestChain_FollowsSampleRateChange(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	got := run(c, toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0), s)
	const half = testRate / 2
	got = append(got, run(c, toFrames(beaconAir(t, half, 2, time.Second), half, sdr.DefaultBlockSize, 1000), s)...)
	checkPackets(t, got, 3)
}

func TestChain_DecodesFSK9600Beacons(t *testing.T) {
	// The recommended 30 kHz and the default 15 kHz channel bandwidth.
	for _, bw := range []uint32{30_000, 15_000} {
		t.Run("", func(t *testing.T) {
			c := NewChain()
			air := beaconAirOf(t, testRate, 3, time.Second, "fsk9600_g3ruh")
			s := fsk9600Settings(testOffset, -90)
			s.BandwidthHz = bw
			got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			checkPackets(t, got, 3)
			if st := c.Status(); st.Good != 3 || !st.Supported {
				t.Errorf("bandwidth %d: status=%+v, want 3 good, supported", bw, st)
			}
		})
	}
}

func TestChain_DecodesGEOSCANBeacons(t *testing.T) {
	c := NewChain()
	air := beaconAirOf(t, testRate, 3, time.Second, "fsk9600_geoscan")
	s := fsk9600Settings(testOffset, -90)
	s.Demod = "fsk9600_geoscan"
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)

	if len(got) != 3 {
		t.Fatalf("packets=%d, want 3", len(got))
	}
	f := testFrame()
	for i, p := range got {
		if p.Frame.Src != f.Src || p.Frame.Dest != f.Dest || !bytes.HasPrefix(p.Frame.Info, f.Info) {
			t.Errorf("packet %d frame=%+v, want %+v padded with zeros", i, p.Frame, f)
		}
	}
	if !bytes.Equal(got[0].Raw, geoscanData(t)) {
		t.Errorf("raw=% X, want frame data without CRC", got[0].Raw)
	}
	if st := c.Status(); st.Good != 3 || !st.Supported {
		t.Errorf("status=%+v, want 3 good, supported", st)
	}
}

func TestChain_ReportsReceptionStages(t *testing.T) {
	// The channel is tuned 1.5 kHz above the beacon: its carrier sits at -1500 Hz in the channel.
	const mistune = 1500
	c := NewChain()
	if st := c.Status(); st.Measured {
		t.Fatalf("status=%+v, want nothing measured before the signal", st)
	}
	air := beaconAirOf(t, testRate, 3, time.Second, "fsk9600_geoscan")
	// The threshold between the noise (-60 dBFS) and the beacon: meters see only the signal.
	s := fsk9600Settings(testOffset+mistune, -50)
	s.Demod = "fsk9600_geoscan"
	if got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s); len(got) != 3 {
		t.Fatalf("packets=%d, want 3", len(got))
	}

	st := c.Status()
	if !st.Measured {
		t.Fatalf("status=%+v, want measured", st)
	}
	if math.Abs(st.CarrierHz+mistune) > 300 {
		t.Errorf("carrier %.0f Hz, want about %d", st.CarrierHz, -mistune)
	}
	if st.DeviationHz < 2200 || st.DeviationHz > 3300 {
		t.Errorf("deviation %.0f Hz, want about 3000", st.DeviationHz)
	}
	if st.ClockJitter > 0.12 {
		t.Errorf("clock jitter %.3f symbol, want locked (< 0.12)", st.ClockJitter)
	}
	if !st.SyncCounted || st.SyncWords != 3 || st.SyncBitErrors > 3 {
		t.Errorf("sync counted/words/bit errors=%v/%d/%d, want true/3/few", st.SyncCounted, st.SyncWords, st.SyncBitErrors)
	}
}

func TestChain_HDLCProfileHasNoSyncCounter(t *testing.T) {
	c := NewChain()
	run(c, toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0), afskSettings(testOffset, -50))
	st := c.Status()
	if !st.Measured || st.SyncCounted || st.DeviationHz != 0 {
		t.Errorf("status=%+v, want measured, no sync counter, no deviation for AFSK", st)
	}
	if st.ClockJitter > 0.12 {
		t.Errorf("clock jitter %.3f symbol, want locked (< 0.12)", st.ClockJitter)
	}
}

func TestChain_FollowsDemodChange(t *testing.T) {
	c := NewChain()
	frames := toFrames(beaconAirOf(t, testRate, 3, time.Second, "fsk9600_g3ruh"), testRate, sdr.DefaultBlockSize, 0)
	var got []Packet
	for i, f := range frames {
		// AFSK demodulator during the first burst, FSK 9600 from 0.9 s; nothing else changes.
		s := fsk9600Settings(testOffset, -90)
		if i*sdr.DefaultBlockSize < testRate*9/10 {
			s.Demod = "afsk1200"
		}
		got = append(got, c.Process(f, s)...)
	}
	checkPackets(t, got, 2)
}

// The receiver panel can only set demodulators from sdr.Demods.
func TestProfiles_AreSelectable(t *testing.T) {
	for name := range profiles {
		if !slices.Contains(sdr.Demods, name) {
			t.Errorf("profile %q is not in sdr.Demods", name)
		}
	}
}

func TestChain_UnsupportedDemodReceivesNothing(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	s.Demod = "gmsk9600"
	frames := toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0)
	if got := run(c, frames, s); len(got) != 0 {
		t.Fatalf("packets=%d, want 0 for a demodulator that is not implemented", len(got))
	}
	if st := c.Status(); st.Supported || st.Demod != "gmsk9600" {
		t.Errorf("status=%+v, want unsupported gmsk9600", st)
	}
}
