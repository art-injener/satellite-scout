package bandscan

import (
	"fmt"
	"math"
	"math/cmplx"
	"math/rand/v2"
	"testing"

	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	testRate     = 2_560_000
	testCenter   = 437_000_000
	testUsableHz = 2_048_000
	noiseAmp     = 0.01
)

// tone is a carrier at offsetHz from the receiver centre with amplitude amp.
type tone struct {
	offsetHz float64
	amp      float64
	phase    float64
}

// iqGen synthesises receiver blocks: complex Gaussian noise plus tones, sample-continuous.
type iqGen struct {
	rng       *rand.Rand
	n         uint64 // samples produced so far
	rate      uint64
	center    uint64
	blockSize int // 0 — sdr.DefaultBlockSize
	tones     []tone
	extra     []complex64 // added when on, indexed by absolute sample number, looped
}

func newIQGen(tones ...tone) *iqGen {
	return &iqGen{rng: rand.New(rand.NewPCG(1, 2)), tones: tones, rate: testRate, center: testCenter}
}

// block returns sdr.DefaultBlockSize samples; on=false mutes the tones (noise only).
func (g *iqGen) block(on bool) sdr.IQFrame {
	n := g.blockSize
	if n <= 0 {
		n = sdr.DefaultBlockSize
	}
	s := make([]complex64, n)
	for i := range s {
		v := complex(g.rng.NormFloat64()*noiseAmp/math.Sqrt2, g.rng.NormFloat64()*noiseAmp/math.Sqrt2)
		if on {
			abs := g.n + uint64(i)
			t := float64(abs) / float64(g.rate)
			for _, tn := range g.tones {
				v += cmplx.Rect(tn.amp, 2*math.Pi*tn.offsetHz*t+tn.phase)
			}
			if len(g.extra) > 0 {
				v += complex128(g.extra[abs%uint64(len(g.extra))])
			}
		}
		s[i] = complex64(v)
	}
	ms := g.n * 1000 / g.rate
	g.n += uint64(len(s))
	return sdr.IQFrame{Samples: s, CenterHz: g.center, SampleRate: uint32(g.rate), TimestampMs: 1_700_000_000_000 + ms}
}

// gfsk9600 is n samples of GFSK 9600 (BT 0.5, deviation ±devHz) at offsetHz from the centre,
// amplitude amp. At ±4.8 kHz (index 1) the spectrum has tone lines with a dip between them.
func gfsk9600(rate, offsetHz, devHz, amp float64, n int) []complex64 {
	m := modem.NewFSK(modem.FSKConfig{SampleRate: rate, Baud: 9600, DeviationHz: devHz, BT: 0.5})
	bits := modem.NewPRBS9()
	levels := make([]byte, int(math.Ceil(float64(n)/(rate/9600)))+8)
	for i := range levels {
		levels[i] = bits.Next()
	}
	iq := m.Modulate(nil, levels)
	if len(iq) < n {
		panic(fmt.Sprintf("gfsk9600: got %d samples, want %d", len(iq), n))
	}
	out := make([]complex64, n)
	for i := range out {
		s := complex128(iq[i]) * cmplx.Rect(amp, 2*math.Pi*offsetHz*float64(i)/rate)
		out[i] = complex64(s)
	}
	return out
}

// wfmNoise is n samples of broadcast-like FM at offsetHz: audio is piecewise-linear random
// in [-1, 1] at 15 kHz, so the spectrum is about flat over ±devHz. Amplitude amp.
func wfmNoise(rate, offsetHz, devHz, amp float64, n int) []complex64 {
	rng := rand.New(rand.NewPCG(5, 6))
	step := rate / 15_000
	audio := make([]float32, n)
	a, b := rng.Float64()*2-1, rng.Float64()*2-1
	for i := range audio {
		k := math.Mod(float64(i), step) / step
		if i > 0 && math.Mod(float64(i), step) < math.Mod(float64(i-1), step) {
			a, b = b, rng.Float64()*2-1
		}
		audio[i] = float32(a + (b-a)*k)
	}
	iq := modem.NewFM(modem.FMConfig{SampleRate: rate, DeviationHz: devHz}).Modulate(nil, audio)
	for i := range iq {
		iq[i] = complex64(complex128(iq[i]) * cmplx.Rect(amp, 2*math.Pi*offsetHz*float64(i)/rate))
	}
	return iq
}

// decide feeds blocks until the detector makes its second decision (the first one only seeds the noise).
func decide(t *testing.T, d *Detector, next func(i int) sdr.IQFrame) Decision {
	t.Helper()
	return decideBand(t, d, testUsableHz, next)
}

func decideBand(t *testing.T, d *Detector, usableHz uint32, next func(i int) sdr.IQFrame) Decision {
	t.Helper()
	got := 0
	for i := range 1000 {
		if dec, ok := d.Push(next(i), usableHz); ok {
			got++
			if got == 2 {
				return dec
			}
		}
	}
	t.Fatal("detector made no decision")
	return Decision{}
}

func peakAt(peaks []Peak, hz float64) (Peak, bool) {
	for _, p := range peaks {
		if p.LoHz <= hz && hz <= p.HiHz {
			return p, true
		}
	}
	return Peak{}, false
}

// The per-interval maximum of pure noise must stay under the threshold, decision after decision.
func TestDetector_NoiseOnlyHasNoPeaks(t *testing.T) {
	g := newIQGen()
	d := NewDetector()
	var dec Decision
	for n := 0; n < 20; {
		var ok bool
		if dec, ok = d.Push(g.block(true), testUsableHz); !ok {
			continue
		}
		n++
		if len(dec.Peaks) != 0 {
			t.Fatalf("decision %d on noise only: peaks %+v, want none", n, dec.Peaks)
		}
	}
	// Noise power per bin is noiseAmp²/N scaled by the window power, well below 0 dBFS.
	if !(dec.NoiseDBFS < -60 && dec.NoiseDBFS > -140) {
		t.Errorf("NoiseDBFS = %.1f, want a finite floor", dec.NoiseDBFS)
	}
}

func TestDetector_FindsCarrierAboveNoise(t *testing.T) {
	g := newIQGen(tone{offsetHz: 100_000, amp: 0.003})
	dec := decide(t, NewDetector(), func(int) sdr.IQFrame { return g.block(true) })
	p, ok := peakAt(dec.Peaks, testCenter+100_000)
	if !ok {
		t.Fatalf("carrier at +100 kHz not found, peaks %+v", dec.Peaks)
	}
	if p.SNRDB < 10 {
		t.Errorf("SNR = %.1f dB, want a clear carrier", p.SNRDB)
	}
	if w := p.HiHz - p.LoHz; w > 10*dec.BinHz {
		t.Errorf("carrier width = %.0f Hz, want a few bins (bin %.0f Hz)", w, dec.BinHz)
	}
	if len(dec.Peaks) != 1 {
		t.Errorf("got %d peaks %+v, want only the carrier", len(dec.Peaks), dec.Peaks)
	}
}

// A burst shorter than the decision interval is caught by the per-interval maximum.
func TestDetector_CatchesBurstShorterThanInterval(t *testing.T) {
	g := newIQGen(tone{offsetHz: -300_000, amp: 0.003})
	d := NewDetector()
	// One block is 6.4 ms of a ~170 ms interval; the burst lasts two blocks.
	dec := decide(t, d, func(i int) sdr.IQFrame { return g.block(i == 30 || i == 31) })
	if _, ok := peakAt(dec.Peaks, testCenter-300_000); !ok {
		t.Errorf("13 ms burst not found, peaks %+v", dec.Peaks)
	}
}

// A steady wideband signal (broadcast FM-like, 150 kHz) is not taken for noise.
func TestDetector_WideSteadySignalIsNotNoise(t *testing.T) {
	var tones []tone
	rng := rand.New(rand.NewPCG(3, 4))
	for f := 400_000.0; f <= 550_000; f += 1_250 {
		tones = append(tones, tone{offsetHz: f, amp: 0.001, phase: rng.Float64() * 2 * math.Pi})
	}
	g := newIQGen(tones...)
	dec := decide(t, NewDetector(), func(int) sdr.IQFrame { return g.block(true) })
	p, ok := peakAt(dec.Peaks, testCenter+475_000)
	if !ok {
		t.Fatalf("wide signal not found, peaks %+v", dec.Peaks)
	}
	if w := p.HiHz - p.LoHz; w < 120_000 {
		t.Errorf("wide signal width = %.0f Hz, want ~150 kHz in one segment", w)
	}
}

func TestDetector_CenterCarrierIsSignalOutOfBandIsNot(t *testing.T) {
	g := newIQGen(tone{offsetHz: 0, amp: 0.01}, tone{offsetHz: 1_150_000, amp: 0.01})
	dec := decide(t, NewDetector(), func(int) sdr.IQFrame { return g.block(true) })
	if _, ok := peakAt(dec.Peaks, testCenter); !ok {
		t.Errorf("centre carrier not found, peaks %+v", dec.Peaks)
	}
	if _, ok := peakAt(dec.Peaks, testCenter+1_150_000); ok {
		t.Errorf("out-of-band carrier reported: %+v", dec.Peaks)
	}
	if lo, hi := dec.UsableHz(); lo > testCenter-1_000_000 || hi < testCenter+1_000_000 || hi-lo > testUsableHz {
		t.Errorf("usable range %.0f..%.0f, want centre ± %d", lo, hi, testUsableHz/2)
	}
}

// One second of RTL-SDR stream (2.56 MS/s): the CPU budget of the detector.
func BenchmarkDetector_OneSecondOfStream(b *testing.B) {
	g := newIQGen(tone{offsetHz: 100_000, amp: 0.003})
	blocks := make([]sdr.IQFrame, testRate/sdr.DefaultBlockSize)
	for i := range blocks {
		blocks[i] = g.block(true)
	}
	d := NewDetector()
	b.ResetTimer()
	for range b.N {
		for _, f := range blocks {
			d.Push(f, testUsableHz)
		}
	}
}

// Retuning drops the accumulated spectrum: the next decision is all at the new frequency.
func TestDetector_RetuneStartsAfresh(t *testing.T) {
	g := newIQGen(tone{offsetHz: 200_000, amp: 0.003})
	d := NewDetector()
	decide(t, d, func(int) sdr.IQFrame { return g.block(true) })
	dec := decide(t, d, func(int) sdr.IQFrame {
		f := g.block(true)
		f.CenterHz = testCenter + 50_000
		return f
	})
	if dec.CenterHz != testCenter+50_000 {
		t.Errorf("CenterHz = %d, want the new centre", dec.CenterHz)
	}
	if _, ok := peakAt(dec.Peaks, testCenter+50_000+200_000); !ok {
		t.Errorf("carrier not at the retuned absolute frequency, peaks %+v", dec.Peaks)
	}
}

// Bin frequency follows the length of Excess, so a short decision is centred on its own DC bin.
func TestDecision_BinFreqFollowsExcessLength(t *testing.T) {
	d := Decision{CenterHz: 1_000_000, BinHz: 10, Excess: make([]float32, 256)}
	if got := d.binFreq(128); got != 1_000_000 {
		t.Fatalf("binFreq(128) = %.0f, want the centre 1000000", got)
	}
	if got := d.binFreq(129); got != 1_000_010 {
		t.Fatalf("binFreq(129) = %.0f, want 1000010", got)
	}
	for i := range d.Excess {
		d.Excess[i] = -1
	}
	d.loBin, d.hiBin = 0, 256
	d.Excess[128] = 12
	if got := d.ExcessIn(999_950, 1_000_050); got != 12 {
		t.Fatalf("ExcessIn = %.1f, want 12 at the centre bin", got)
	}
}

// FFT length tracks the sample rate: 2.56 MS/s keeps 4096 (625 Hz), 256 kS/s is 1024 (250 Hz),
// a 57.6 kS/s recording is 256 (225 Hz).
func TestDetector_BinHzFollowsSampleRate(t *testing.T) {
	cases := []struct {
		rate uint32
		bin  float64
	}{
		{2_560_000, 625},
		{256_000, 250},
		{57_600, 225},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d", tc.rate), func(t *testing.T) {
			g := newIQGen()
			g.rate = uint64(tc.rate)
			dec := decideBand(t, NewDetector(), tc.rate, func(int) sdr.IQFrame { return g.block(true) })
			if math.Abs(dec.BinHz-tc.bin) > 0.01 {
				t.Errorf("BinHz = %.2f at %d S/s, want %.0f", dec.BinHz, tc.rate, tc.bin)
			}
		})
	}
}

// A recording at 57.6 kS/s must not turn pure noise into hundreds of 14 Hz segments.
func TestDetector_LowRateNoiseHasNoPeaks(t *testing.T) {
	const rate = uint32(57_600)
	g := newIQGen()
	g.rate = uint64(rate)
	g.blockSize = int(rate) / 20 // as a replay block: one decision is several blocks, not one
	d := NewDetector()
	for n := 0; n < 60; {
		dec, ok := d.Push(g.block(true), rate)
		if !ok {
			continue
		}
		n++
		if len(dec.Peaks) != 0 {
			t.Fatalf("decision %d on noise at %d S/s: %d peaks, first %.0f..%.0f Hz (bin %.1f Hz)",
				n, rate, len(dec.Peaks), dec.Peaks[0].LoHz, dec.Peaks[0].HiHz, dec.BinHz)
		}
	}
}

// One GFSK 9600 in a 57.6 kS/s recording is one segment, not a comb of noise bins.
func TestDetector_LowRateFSKIsOneSegment(t *testing.T) {
	const rate = uint32(57_600)
	const offsetHz = 8_000.0
	g := newIQGen()
	g.rate = uint64(rate)
	g.center = 436_200_000
	g.blockSize = int(rate) / 20
	g.extra = gfsk9600(float64(rate), offsetHz, 3000, 0.05, int(rate))
	dec := decideBand(t, NewDetector(), rate, func(int) sdr.IQFrame { return g.block(true) })
	freq := float64(g.center) + offsetHz
	if len(dec.Peaks) != 1 {
		t.Fatalf("got %d peaks at %d S/s, want one FSK segment (bin %.1f Hz): %+v",
			len(dec.Peaks), rate, dec.BinHz, dec.Peaks)
	}
	p := dec.Peaks[0]
	if p.LoHz > freq || p.HiHz < freq {
		t.Errorf("segment %.0f..%.0f Hz does not cover FSK at %.0f", p.LoHz, p.HiHz, freq)
	}
	if w := p.HiHz - p.LoHz; w > 40_000 {
		t.Errorf("width = %.0f Hz, want the FSK, not the whole band", w)
	}
}

// segmentsAround runs decisions 3..8 and checks each one has exactly one segment within
// [lo, hi] (offsets from the centre), covering the middle and at least minWidth wide.
func segmentsAround(t *testing.T, g *iqGen, usableHz uint32, lo, hi, minWidth float64) {
	t.Helper()
	d := NewDetector()
	mid := (lo + hi) / 2
	for n := 0; n < 8; {
		dec, ok := d.Push(g.block(true), usableHz)
		if !ok {
			continue
		}
		n++
		if n < 3 {
			continue
		}
		var in []Peak
		for _, p := range dec.Peaks {
			if c := p.CenterHz() - float64(dec.CenterHz); c >= lo && c <= hi {
				in = append(in, p)
			}
		}
		if len(in) != 1 {
			t.Fatalf("decision %d: %d segments in %.0f..%.0f Hz, want one (bin %.1f Hz): %+v",
				n, len(in), lo, hi, dec.BinHz, in)
		}
		p := in[0]
		c := float64(dec.CenterHz) + mid
		if p.LoHz > c || p.HiHz < c {
			t.Errorf("decision %d: segment %.0f..%.0f does not cover %.0f", n, p.LoHz, p.HiHz, c)
		}
		if w := p.HiHz - p.LoHz; w < minWidth {
			t.Errorf("decision %d: width %.0f Hz, want at least %.0f", n, w, minWidth)
		}
	}
}

// A weak GFSK 9600 with tone lines at ±4.8 kHz (as GEOSCAN): bins between the tones dip
// under the threshold, but it is still one segment, not a peak per tone.
func TestDetector_WeakFSKWithToneLinesIsOneSegment(t *testing.T) {
	const rate = 57_600
	g := newIQGen()
	g.rate, g.center, g.blockSize = rate, 436_200_000, rate/20
	g.extra = gfsk9600(rate, 8_000, 4_800, 0.01, rate)
	segmentsAround(t, g, rate, 0, 16_000, 8_000)
}

// Weak broadcast FM (6 dB per bin at best) is one wide segment, not pieces of 1–5 kHz.
func TestDetector_WeakWideFMIsOneSegment(t *testing.T) {
	g := newIQGen()
	g.extra = wfmNoise(testRate, 500_000, 75_000, 0.004, testRate)
	segmentsAround(t, g, testUsableHz, 400_000, 600_000, 120_000)
}

// Two carriers with noise between them stay two segments at both rates.
func TestDetector_NeighbourCarriersStaySeparate(t *testing.T) {
	cases := []struct {
		name   string
		rate   uint64
		usable uint32
		a, b   float64
		amp    float64
	}{
		{"2.56MSps_5kHz", testRate, testUsableHz, 100_000, 105_000, 0.003},
		{"57.6kSps_2.5kHz", 57_600, 57_600, 8_000, 10_500, 0.01},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := newIQGen(tone{offsetHz: tc.a, amp: tc.amp}, tone{offsetHz: tc.b, amp: tc.amp})
			g.rate = tc.rate
			if tc.rate != testRate {
				g.blockSize = int(tc.rate) / 20
			}
			dec := decideBand(t, NewDetector(), tc.usable, func(int) sdr.IQFrame { return g.block(true) })
			_, okA := peakAt(dec.Peaks, testCenter+tc.a)
			_, okB := peakAt(dec.Peaks, testCenter+tc.b)
			if len(dec.Peaks) != 2 || !okA || !okB {
				t.Errorf("got %d peaks %+v, want the two carriers apart (bin %.1f Hz)", len(dec.Peaks), dec.Peaks, dec.BinHz)
			}
		})
	}
}

// A strong broadcast-FM hump with a weak edge across a few-kHz dip is one station, not two.
func TestMergeNearbyPeaks_JoinsWeakEdgeOfWideFM(t *testing.T) {
	in := []Peak{
		{LoHz: 103_620_000, HiHz: 103_800_000, SNRDB: 22, Power: 100, CentroidHz: 103_710_000},
		{LoHz: 103_812_000, HiHz: 103_840_000, SNRDB: 7, Power: 2, CentroidHz: 103_826_000},
	}
	got := mergeNearbyPeaks(in)
	if len(got) != 1 {
		t.Fatalf("got %d peaks, want one station: %+v", len(got), got)
	}
	if got[0].LoHz != in[0].LoHz || got[0].HiHz != in[1].HiHz {
		t.Errorf("merged %+v, want %.0f..%.0f", got[0], in[0].LoHz, in[1].HiHz)
	}
	if got[0].SNRDB != 22 {
		t.Errorf("SNR %.0f, want the strong hump", got[0].SNRDB)
	}
}

// Neighbouring FM stations ~400 kHz apart stay separate even if each is ~180 kHz wide.
func TestMergeNearbyPeaks_KeepsNeighbourStationsApart(t *testing.T) {
	in := []Peak{
		{LoHz: 103_620_000, HiHz: 103_800_000, SNRDB: 22, Power: 50, CentroidHz: 103_710_000},
		{LoHz: 104_020_000, HiHz: 104_180_000, SNRDB: 20, Power: 40, CentroidHz: 104_100_000},
	}
	got := mergeNearbyPeaks(in)
	if len(got) != 2 {
		t.Fatalf("got %d peaks, want two stations: %+v", len(got), got)
	}
}

// Changing the sample rate rebuilds the FFT; the next decision uses the new bin width.
func TestDetector_RateChangeResizesFFT(t *testing.T) {
	g := newIQGen()
	d := NewDetector()
	decide(t, d, func(int) sdr.IQFrame { return g.block(true) })
	g.rate = 57_600
	g.n = 0
	dec := decideBand(t, d, 57_600, func(int) sdr.IQFrame { return g.block(true) })
	if math.Abs(dec.BinHz-225) > 0.01 {
		t.Errorf("BinHz after rate change = %.2f, want 225", dec.BinHz)
	}
}
