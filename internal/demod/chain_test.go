package demod

import (
	"bytes"
	"encoding/hex"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/ax100"
	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/ccsds"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/m10"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/payload"
	"github.com/art-injener/satellite-scout/internal/rs41"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/usp"
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
	return burstLevelsFlags(t, 32, 2)
}

func burstLevelsFlags(t *testing.T, preamble, postamble int) []byte {
	t.Helper()
	b, err := testFrame().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return ax25.NRZI(ax25.HDLCBits(b, preamble, postamble))
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

// modulateBurst — one beacon frame in AFSK 1200, FSK 1200/4800, FSK 9600 G3RUH or GEOSCAN.
func modulateBurst(t *testing.T, rate float64, demod string) []complex64 {
	t.Helper()
	switch demod {
	case "fsk1200", "gfsk1200":
		return modem.NewFSK(modem.GFSK1200(rate)).Modulate(nil, burstLevels(t))
	case "fsk4800", "gfsk4800":
		return modem.NewFSK(modem.GFSK4800(rate)).Modulate(nil, burstLevels(t))
	case "gfsk9600":
		var s modem.G3RUHScrambler
		return modem.NewFSK(modem.GFSK9600(rate)).Modulate(nil, s.Scramble(nil, burstLevels(t)))
	case "gmsk9600":
		var s modem.G3RUHScrambler
		// 64 флага: у GMSK захват дольше, чем у GFSK (как у маяка имитатора).
		return modem.NewFSK(modem.GMSK9600(rate)).Modulate(nil, s.Scramble(nil, burstLevelsFlags(t, 64, 2)))
	case "gmsk4800":
		return modem.NewFSK(modem.GMSK4800(rate)).Modulate(nil, burstLevelsFlags(t, 64, 2))
	case "bpsk1200":
		return modem.NewBPSK(modem.BPSK1200(rate)).Modulate(nil, burstLevelsFlags(t, 64, 2))
	case "bpsk9600":
		return modem.NewBPSK(modem.BPSK9600(rate)).Modulate(nil, burstLevelsFlags(t, 64, 2))
	case "qpsk_ccsds":
		levels, err := ccsds.Encode(ccsdsTestData(t), 64, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewQPSK(modem.QPSKCCSDS(rate)).Modulate(nil, levels)
	case "fsk9600_g3ruh":
		var s modem.G3RUHScrambler
		return modem.NewFSK(modem.FSK9600(rate)).Modulate(nil, s.Scramble(nil, burstLevels(t)))
	case "fsk9600_geoscan":
		levels, err := geoscan.Encode(geoscanData(t), 32, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.FSK9600(rate)).Modulate(nil, levels)
	case "gmsk4800_ax100":
		levels, err := ax100.Encode(ax100TestCSP(t), 64, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.GMSK4800(rate)).Modulate(nil, levels)
	case "gmsk9600_ax100":
		levels, err := ax100.Encode(ax100TestCSP(t), 64, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.GMSK9600(rate)).Modulate(nil, levels)
	case "fsk4800_ax100":
		levels, err := ax100.Encode(ax100TestCSP(t), 32, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.FSK4800(rate)).Modulate(nil, levels)
	case "gmsk9600_ax100_rs":
		levels, err := ax100.EncodeRSMode(ax100TestCSP(t), 64, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.GMSK9600(rate)).Modulate(nil, levels)
	case "fsk9600_ax100_rs":
		levels, err := ax100.EncodeRSMode(ax100TestCSP(t), 32, 2)
		if err != nil {
			t.Fatal(err)
		}
		return modem.NewFSK(modem.FSK9600(rate)).Modulate(nil, levels)
	case "gmsk2400_usp", "gmsk4800_usp", "gmsk9600_usp":
		raw, err := usp.SampleGeneralAX25()
		if err != nil {
			t.Fatal(err)
		}
		// Длинный PLS: маяк 0x4216 не помещается в короткий кадр USP.
		levels, err := usp.Encode(raw, true, 64, 4)
		if err != nil {
			t.Fatal(err)
		}
		switch demod {
		case "gmsk2400_usp":
			return modem.NewFSK(modem.GMSK2400(rate)).Modulate(nil, levels)
		case "gmsk4800_usp":
			return modem.NewFSK(modem.GMSK4800(rate)).Modulate(nil, levels)
		default:
			return modem.NewFSK(modem.GMSK9600(rate)).Modulate(nil, levels)
		}
	case "rs41":
		clear, err := hex.DecodeString(rs41GoldenClearHex)
		if err != nil {
			t.Fatal(err)
		}
		pre := make([]byte, 320)
		for i := range pre {
			pre[i] = byte((i + 1) % 2) // преамбула 0101…, заканчивается единицей
		}
		levels := append(pre, rs41.BytesToLevels(rs41.ScrambleFrame(clear))...)
		levels = append(levels, make([]byte, 64)...)
		return modem.NewFSK(modem.GFSK4800(rate)).Modulate(nil, levels)
	case "m10":
		raw, err := hex.DecodeString(m10GoldenHex)
		if err != nil {
			t.Fatal(err)
		}
		pre := make([]byte, 128)
		for i := range pre {
			pre[i] = byte(i % 2)
		}
		levels := append(pre, m10.BytesToLevels(raw)...)
		levels = append(levels, make([]byte, 128)...)
		return modem.NewFSK(modem.FSKConfig{
			SampleRate: rate, Baud: m10.Baud, DeviationHz: 3000, BT: 0.5,
		}).Modulate(nil, levels)
	}
	return modem.NewAFSK(modem.AFSK1200(rate)).Modulate(nil, burstLevels(t))
}

// rs41GoldenClearHex — эталонный кадр rs1729 (после whitening), CRC TLV ок.
const rs41GoldenClearHex = "8635f44093df1a602c87e0fa0521e8943d9cef4c7a67393f6d39fb546461f2111b6447ab79a746c80350cda5344157f8c0c12234f46902220f792816174b313933303239331a00000300000a00002f0007322ce53e31991abf12dada3eb68468c16755d51c7a2a15310216060245f302000d08a31607821e08bb210219060243f302000000000000000000000000000000220d7c1e0807d03cdc071fd81ddb19d70a8d0eb602b60cb518d40692ff00ff00ff001c277d59b8d83301ff0f881f0f38f4fe18b283038735ff000000003eb8ff4947201e6e3aff55415f13fc6e005440440cf100009e9f7406f85800832b631719d70010bebc172a8b00000000000000000000000000000000000000000000a48b7b15366181193ef05d07e1245b1be0f721f801f60804107b0b76110000000000000000000000000000000000ecc7"

const m10GoldenHex = "649f20000000000000000000000020bf258c0b78607c004c4b400000000000000000000000000000000000000000000000000000000000000000000000000106a20000000000000000000000000000000000000000000000000000000002083a3125019f37"

func ax100TestCSP(t *testing.T) []byte {
	t.Helper()
	return ax100.PackSuchaiBeacon(ax100.SuchaiPort2, map[string]uint32{
		"eps_vbatt":     7420,
		"obc_hrs_alive": 99,
		"timestamp":     1_700_000_000,
	})
}

func ccsdsTestData(t *testing.T) []byte {
	t.Helper()
	return []byte("scout ccsds cadu chain probe")
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

func TestChain_DecodesBPSK1200Beacons(t *testing.T) {
	c := NewChain()
	air := beaconAirWithLead(t, testRate, 3, time.Second, "bpsk1200", testRate/5)
	s := afskSettings(testOffset, -90)
	s.Demod = "bpsk1200"
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	checkPackets(t, got, 3)
	if st := c.Status(); st.Good != 3 || !st.Supported {
		t.Errorf("status=%+v, want 3 good, supported", st)
	}
}

func TestChain_DecodesBPSK9600Beacons(t *testing.T) {
	c := NewChain()
	air := beaconAirWithLead(t, testRate, 3, time.Second, "bpsk9600", testRate/5)
	s := afskSettings(testOffset, -90)
	s.Demod = "bpsk9600"
	s.BandwidthHz = sdr.MaxChannelBandwidthHz
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	checkPackets(t, got, 3)
	if st := c.Status(); st.Good != 3 || !st.Supported {
		t.Errorf("status=%+v, want 3 good, supported", st)
	}
	if !slices.Contains(ImplementedFrameProfiles(), "bpsk9600") {
		t.Error("bpsk9600 must be in ImplementedFrameProfiles")
	}
	pl := payload.Decode(payload.Frame{Raw: got[0].Raw, Profile: "bpsk9600"})
	if pl.Kind != payload.KindText {
		t.Fatalf("payload kind=%q decoder=%q, want text (AX.25 UI)", pl.Kind, pl.Decoder)
	}
}

func TestChain_MSK9600SupportedLine(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	s.Demod = "msk9600"
	s.BandwidthHz = sdr.MaxChannelBandwidthHz
	cfg := modem.MSK9600(testRate)
	levels := make([]byte, 5000)
	for i := range levels {
		levels[i] = byte(i % 2)
	}
	air := toAir(modem.NewFSK(cfg).Modulate(nil, levels), testRate, 20)
	_ = run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	if st := c.Status(); !st.Supported || st.Demod != "msk9600" {
		t.Errorf("status=%+v, want supported msk9600", st)
	}
}

func TestChain_MFSKLineSupported(t *testing.T) {
	for _, tc := range []struct {
		demod string
		cfg   modem.MFSKConfig
	}{
		{"3fsk4800", modem.FSK3_4800(testRate)},
		{"4fsk4800", modem.FSK4DMR4800(testRate)},
		{"gmfsk4800", modem.GMFSK4800(testRate)},
	} {
		t.Run(tc.demod, func(t *testing.T) {
			c := NewChain()
			s := afskSettings(testOffset, -90)
			s.Demod = tc.demod
			s.BandwidthHz = sdr.MaxChannelBandwidthHz
			levels := make([]byte, 3000)
			for i := range levels {
				levels[i] = byte(i % tc.cfg.Levels)
			}
			air := toAir(modem.NewMFSK(tc.cfg).Modulate(nil, levels), testRate, 20)
			_ = run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			if st := c.Status(); !st.Supported || st.Demod != tc.demod {
				t.Errorf("status=%+v, want supported %s", st, tc.demod)
			}
			if slices.Contains(ImplementedFrameProfiles(), tc.demod) {
				t.Errorf("%s must not be in ImplementedFrameProfiles", tc.demod)
			}
		})
	}
}

func TestChain_DecodesQPSKCCSDS(t *testing.T) {
	c := NewChain()
	// Тишина до кадра: Costas/ФАПЧ тактов успевают захватить до ASM.
	air := beaconAirWithLead(t, testRate, 2, time.Second, "qpsk_ccsds", testRate/5)
	s := afskSettings(testOffset, -90)
	s.Demod = "qpsk_ccsds"
	s.BandwidthHz = sdr.MaxChannelBandwidthHz
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	if len(got) == 0 {
		t.Fatalf("no frames, status=%+v", c.Status())
	}
	want := ccsdsTestData(t)
	if !bytes.HasPrefix(got[0].Raw, want) {
		t.Fatalf("raw prefix=%q, want %q", got[0].Raw[:min(len(got[0].Raw), len(want))], want)
	}
	if st := c.Status(); st.Good < 1 || !st.Supported || !st.FECCounted {
		t.Fatalf("status=%+v, want good>=1 supported FECCounted", st)
	}
	if !slices.Contains(ImplementedFrameProfiles(), "qpsk_ccsds") {
		t.Error("qpsk_ccsds must be in ImplementedFrameProfiles")
	}
	pl := payload.Decode(payload.Frame{Raw: got[0].Raw, Profile: "qpsk_ccsds"})
	if pl.Kind != payload.KindUnknown {
		t.Fatalf("payload kind=%q decoder=%q, want unknown (no TM parser yet)", pl.Kind, pl.Decoder)
	}
}

// GMSK: тот же FSKDemod, BT 0,4; 9600 — HDLC+G3RUH, 4800 — HDLC без скремблера.
func TestChain_DecodesGMSKBeacons(t *testing.T) {
	for _, tc := range []struct {
		demod string
		scram bool
		lead  bool
	}{
		{"gmsk9600", true, false},
		{"gmsk4800", false, true},
	} {
		t.Run(tc.demod, func(t *testing.T) {
			c := NewChain()
			var air []complex64
			if tc.lead {
				air = beaconAirWithLead(t, testRate, 3, time.Second, tc.demod, testRate/5)
			} else {
				air = beaconAirOf(t, testRate, 3, time.Second, tc.demod)
			}
			s := fsk9600Settings(testOffset, -90)
			s.Demod = tc.demod
			got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			checkPackets(t, got, 3)
			if st := c.Status(); st.Good != 3 || !st.Supported {
				t.Errorf("status=%+v, want 3 good, supported", st)
			}
		})
	}
}

// FSK 1200/4800: GFSK без G3RUH, кадр AX.25 HDLC. У 1200 перед первым кадром —
// короткая тишина: иначе FIR/дискриминатор стартуют на фронте преамбулы.
func TestChain_DecodesFSK1200And4800Beacons(t *testing.T) {
	for _, demod := range []string{"fsk1200", "fsk4800"} {
		t.Run(demod, func(t *testing.T) {
			c := NewChain()
			var air []complex64
			if demod == "fsk1200" {
				air = beaconAirWithLead(t, testRate, 3, time.Second, demod, testRate/5)
			} else {
				air = beaconAirOf(t, testRate, 3, time.Second, demod)
			}
			s := afskSettings(testOffset, -90)
			s.Demod = demod
			if demod == "fsk4800" {
				s.BandwidthHz = sdr.MaxChannelBandwidthHz
			}
			got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			checkPackets(t, got, 3)
			if st := c.Status(); st.Good != 3 || !st.Supported {
				t.Errorf("status=%+v, want 3 good, supported", st)
			}
		})
	}
}

// Явные профили GFSK (BT 0,5): те же пресеты, что fsk*, отдельные id в селекторе.
// 1200/4800 — HDLC без G3RUH; 9600 — HDLC+G3RUH, как fsk9600_g3ruh.
func TestChain_DecodesGFSKBeacons(t *testing.T) {
	for _, tc := range []struct {
		demod string
		wide  bool
		lead  bool
	}{
		{"gfsk1200", false, true},
		{"gfsk4800", true, false},
		{"gfsk9600", true, false},
	} {
		t.Run(tc.demod, func(t *testing.T) {
			c := NewChain()
			var air []complex64
			if tc.lead {
				air = beaconAirWithLead(t, testRate, 3, time.Second, tc.demod, testRate/5)
			} else {
				air = beaconAirOf(t, testRate, 3, time.Second, tc.demod)
			}
			s := afskSettings(testOffset, -90)
			s.Demod = tc.demod
			if tc.wide {
				s.BandwidthHz = sdr.MaxChannelBandwidthHz
			}
			got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			checkPackets(t, got, 3)
			if st := c.Status(); st.Good != 3 || !st.Supported {
				t.Errorf("status=%+v, want 3 good, supported", st)
			}
		})
	}
}

// beaconAirWithLead — как beaconAirOf, плюс lead отсчётов тишины до первого кадра.
func beaconAirWithLead(t *testing.T, rate float64, bursts int, period time.Duration, demod string, lead int) []complex64 {
	t.Helper()
	burst := modulateBurst(t, rate, demod)
	clean := make([]complex64, lead+int(float64(bursts)*period.Seconds()*rate))
	for k := range bursts {
		copy(clean[lead+int(float64(k)*period.Seconds()*rate):], burst)
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

func TestChain_DecodesAX100Beacons(t *testing.T) {
	for _, tc := range []struct {
		demod string
		lead  bool
		wide  bool
	}{
		{"gmsk4800_ax100", true, false},
		{"gmsk9600_ax100", false, true},
		{"fsk4800_ax100", false, false},
		{"gmsk9600_ax100_rs", false, true},
		{"fsk9600_ax100_rs", false, true},
	} {
		t.Run(tc.demod, func(t *testing.T) {
			c := NewChain()
			var air []complex64
			if tc.lead {
				air = beaconAirWithLead(t, testRate, 3, time.Second, tc.demod, testRate/5)
			} else {
				air = beaconAirOf(t, testRate, 3, time.Second, tc.demod)
			}
			s := fsk9600Settings(testOffset, -90)
			s.Demod = tc.demod
			if !tc.wide {
				s.BandwidthHz = 15_000
			}
			got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			if len(got) == 0 {
				t.Fatalf("no frames, status=%+v", c.Status())
			}
			want := ax100TestCSP(t)
			if !bytes.Equal(got[0].Raw, want) {
				t.Fatalf("raw %x, want %x", got[0].Raw, want)
			}
			if st := c.Status(); st.Good < 1 || !st.Supported || !st.FECCounted {
				t.Fatalf("status=%+v, want good>=1 supported FECCounted", st)
			}
			pl := payload.Decode(payload.Frame{Raw: got[0].Raw, Profile: tc.demod})
			if pl.Decoder != "suchai.beacon" || pl.Kind != payload.KindBeacon {
				t.Fatalf("payload kind=%q decoder=%q summary=%q", pl.Kind, pl.Decoder, pl.Summary)
			}
			if !hasParam(pl, "eps_vbatt") {
				t.Fatalf("params=%v, want eps_vbatt", pl.Params)
			}
		})
	}
}

func hasParam(pl payload.Payload, id string) bool {
	for _, p := range pl.Params {
		if p.ID == id {
			return true
		}
	}
	return false
}

func TestChain_DecodesUSPBeacons(t *testing.T) {
	// Длинный USP-кадр на 2400 бод ~2 с — период 3 с, иначе вспышки накладываются.
	const uspPeriod = 3 * time.Second
	for _, tc := range []struct {
		demod string
		lead  bool
		wide  bool
	}{
		{"gmsk2400_usp", true, false},
		{"gmsk4800_usp", true, false},
		{"gmsk9600_usp", false, true},
	} {
		t.Run(tc.demod, func(t *testing.T) {
			c := NewChain()
			var air []complex64
			if tc.lead {
				air = beaconAirWithLead(t, testRate, 2, uspPeriod, tc.demod, testRate/5)
			} else {
				air = beaconAirOf(t, testRate, 2, uspPeriod, tc.demod)
			}
			s := fsk9600Settings(testOffset, -90)
			s.Demod = tc.demod
			if !tc.wide {
				s.BandwidthHz = 15_000
			}
			got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
			if len(got) == 0 {
				t.Fatalf("no frames, status=%+v", c.Status())
			}
			want, err := usp.SampleGeneralAX25()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got[0].Raw, want) {
				t.Fatalf("raw %x want %x", got[0].Raw, want)
			}
			if st := c.Status(); !st.FECCounted {
				t.Fatalf("status=%+v, want FECCounted for USP", st)
			}
			pl := payload.Decode(payload.Frame{Raw: got[0].Raw, Profile: tc.demod})
			if pl.Decoder != "usp.beacon" || pl.Kind != payload.KindBeacon {
				t.Fatalf("payload kind=%q decoder=%q", pl.Kind, pl.Decoder)
			}
			if !hasParam(pl, "uab") {
				t.Fatalf("params=%v, want uab", pl.Params)
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

func TestChain_ReportsSNR(t *testing.T) {
	// Beacon at testSNR=20 dB in 15 kHz: AFSK tones are narrow, so the noise floor
	// by FFT percentile reads clean and SNR lands near the set value.
	c := NewChain()
	air := beaconAir(t, testRate, 3, time.Second)
	s := afskSettings(testOffset, -50)
	if got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s); len(got) != 3 {
		t.Fatalf("packets=%d, want 3", len(got))
	}
	st := c.Status()
	if !st.Measured {
		t.Fatalf("status=%+v, want measured", st)
	}
	if st.SNRDB < 14 || st.SNRDB > 26 {
		t.Errorf("SNR=%.1f dB, want about %.0f", st.SNRDB, testSNR)
	}
}

// Without a signal the SNR stays at the floor: measure() runs only on signal blocks.
func TestChain_SNRUnmeasuredBeforeSignal(t *testing.T) {
	c := NewChain()
	run(c, toFrames(beaconAir(t, testRate, 2, time.Second), testRate, sdr.DefaultBlockSize, 0), afskSettings(testOffset, testLevel+20))
	st := c.Status()
	if st.Measured || st.SNRDB != 0 {
		t.Errorf("status=%+v, want not measured and SNR 0 before any signal", st)
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
	s.Demod = "lora" // нет в profiles
	frames := toFrames(beaconAir(t, testRate, 1, time.Second), testRate, sdr.DefaultBlockSize, 0)
	if got := run(c, frames, s); len(got) != 0 {
		t.Fatalf("packets=%d, want 0 for a demodulator that is not implemented", len(got))
	}
	if st := c.Status(); st.Supported || st.Demod != "lora" {
		t.Errorf("status=%+v, want unsupported lora", st)
	}
}

func TestChain_DecodesRS41(t *testing.T) {
	c := NewChain()
	air := beaconAirOf(t, testRate, 1, time.Second, "rs41")
	s := fsk9600Settings(testOffset, -90)
	s.Demod = "rs41"
	s.BandwidthHz = 15_000
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	if len(got) == 0 {
		t.Fatalf("no frames, status=%+v", c.Status())
	}
	tel, ok := rs41.Parse(got[0].Raw)
	if !ok || tel.ID != "K1930293" {
		t.Fatalf("parse=%+v ok=%v", tel, ok)
	}
	if st := c.Status(); st.Good < 1 || !st.Supported || st.Demod != "rs41" {
		t.Fatalf("status=%+v", st)
	}
}

func TestChain_M10ProfileFramed(t *testing.T) {
	// Кадр/checksum — в internal/m10. Через FSKDemod длинные нули без whitening
	// срывают такты (в отличие от RS41); здесь проверяем профиль в «Авто».
	if !slices.Contains(ImplementedFrameProfiles(), "m10") {
		t.Fatal("m10 not in ImplementedFrameProfiles")
	}
	c := NewChain()
	s := fsk9600Settings(testOffset, -90)
	s.Demod = "m10"
	s.BandwidthHz = 15_000
	_ = c.Process(sdr.IQFrame{Samples: make([]complex64, 1024), SampleRate: testRate}, s)
	if st := c.Status(); !st.Supported || st.Demod != "m10" {
		t.Fatalf("status=%+v", st)
	}
}

func TestChain_OOKSupportedLineBurst(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	s.Demod = "ook"
	s.Baud = 2000
	cfg := modem.OOKConfig{SampleRate: testRate, Baud: 2000}
	// Пачка mark/space, затем длинная пауза (нули) — burstDeframer отдаёт Packet.
	levels := make([]byte, 0, 200)
	for i := 0; i < 64; i++ {
		levels = append(levels, byte(i%2))
	}
	for i := 0; i < 80; i++ {
		levels = append(levels, 0)
	}
	air := toAir(modem.NewOOK(cfg).Modulate(nil, levels), testRate, 20)
	// Тишина после сигнала, чтобы порог закрылся и Flush отдал хвост.
	quiet := make([]complex64, int(testRate/10))
	air = append(air, quiet...)
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	if len(got) < 1 {
		t.Fatalf("packets=%d, want ≥1 line burst", len(got))
	}
	if len(got[0].Raw) < 4 {
		t.Fatalf("raw len=%d, want packed bytes", len(got[0].Raw))
	}
	if st := c.Status(); !st.Supported || st.Demod != "ook" || st.Good < 1 {
		t.Errorf("status=%+v, want supported ook with good≥1", st)
	}
}

func TestChain_PI4DQPSKSupportedLineBurst(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	s.Demod = "pi4_dqpsk"
	s.Baud = 9600
	s.BandwidthHz = sdr.MaxChannelBandwidthHz
	cfg := modem.PI4DQPSKConfig{SampleRate: testRate, Baud: 9600}
	levels := make([]byte, 64)
	for i := range levels {
		levels[i] = 1
	}
	for i := 0; i < 80; i++ {
		levels = append(levels, 0)
	}
	air := toAir(modem.NewPI4DQPSK(cfg).Modulate(nil, levels), testRate, 20)
	air = append(air, make([]complex64, int(testRate/10))...)
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	if len(got) < 1 {
		t.Fatalf("packets=%d, want ≥1 line burst", len(got))
	}
	if st := c.Status(); !st.Supported || st.Demod != "pi4_dqpsk" {
		t.Errorf("status=%+v, want supported pi4_dqpsk", st)
	}
}

func TestChain_APSK16SupportedLineBurst(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	s.Demod = "apsk16"
	s.Baud = 9600
	s.BandwidthHz = sdr.MaxChannelBandwidthHz
	cfg := modem.APSK16Config{SampleRate: testRate, Baud: 9600}
	levels := make([]byte, 64)
	for i := range levels {
		levels[i] = byte(i % 2)
	}
	if levels[0] == 0 {
		levels[0] = 1
	}
	for i := 0; i < 80; i++ {
		levels = append(levels, 0)
	}
	air := toAir(modem.NewAPSK16(cfg).Modulate(nil, levels), testRate, 20)
	air = append(air, make([]complex64, int(testRate/10))...)
	got := run(c, toFrames(air, testRate, sdr.DefaultBlockSize, 0), s)
	if len(got) < 1 {
		t.Fatalf("packets=%d, want ≥1 line burst", len(got))
	}
	if st := c.Status(); !st.Supported || st.Demod != "apsk16" {
		t.Errorf("status=%+v, want supported apsk16", st)
	}
}

func TestChain_OOKRebuildsOnBaudChange(t *testing.T) {
	c := NewChain()
	s := afskSettings(testOffset, -90)
	s.Demod = "ook"
	s.Baud = 480
	cfg := modem.OOKConfig{SampleRate: testRate, Baud: 480}
	levels := make([]byte, 2000)
	for i := range levels {
		levels[i] = byte(i % 2)
	}
	air := toAir(modem.NewOOK(cfg).Modulate(nil, levels), testRate, 20)
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)
	_ = run(c, frames[:1], s)
	if c.baud != 480 {
		t.Fatalf("baud after build=%.0f, want 480", c.baud)
	}
	s.Baud = 422
	_ = run(c, frames[1:2], s)
	if c.baud != 422 {
		t.Fatalf("baud after change=%.0f, want 422", c.baud)
	}
	if st := c.Status(); !st.Supported {
		t.Errorf("status=%+v, want supported after baud change", st)
	}
}

func TestChain_OffsetChangeDoesNotRebuildFilters(t *testing.T) {
	air := beaconAirOf(t, testRate, 2, time.Second, "gmsk9600_usp")
	frames := toFrames(air, testRate, sdr.DefaultBlockSize, 0)
	c := NewChain()
	s := sdr.ChannelSettings{
		OffsetHz:    testOffset - 30_000,
		BandwidthHz: sdr.MaxChannelBandwidthHz,
		SquelchDBFS: -90,
		Demod:       "gmsk9600_usp",
	}
	c.Process(frames[0], s)
	origCoarse, origFine, origDemod := c.coarse, c.fine, c.demod
	if origCoarse == nil || origFine == nil || origDemod == nil {
		t.Fatal("filters and demod must be initialized after first block")
	}

	// Retune channel offset to the actual signal without changing sample rate, bandwidth or demod.
	s.OffsetHz = testOffset
	got := make([]Packet, 0, 2)
	for _, f := range frames[1:] {
		got = append(got, c.Process(f, s)...)
	}
	if c.coarse != origCoarse || c.fine != origFine || c.demod != origDemod {
		t.Fatal("changing only OffsetHz must shift NCO without rebuilding coarse/fine/demod")
	}
	if len(got) == 0 {
		t.Fatal("expected decoded gmsk9600_usp frames after smooth OffsetHz shift")
	}
}
