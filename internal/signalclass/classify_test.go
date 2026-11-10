package signalclass

import (
	"fmt"
	"math"
	"math/cmplx"
	"math/rand/v2"
	"testing"

	"github.com/art-injener/satellite-scout/internal/dsp"
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

const testRate = 48_000.0

func prbs(n int) []byte {
	p := modem.NewPRBS9()
	out := make([]byte, n)
	for i := range out {
		out[i] = p.Next()
	}
	return out
}

func carrier(n int, offsetHz float64) []complex64 {
	out := make([]complex64, n)
	for i := range out {
		out[i] = complex64(cmplx.Rect(1, 2*math.Pi*offsetHz*float64(i)/testRate))
	}
	return out
}

func gfsk(n int, baud, devHz float64) []complex64 {
	m := modem.NewFSK(modem.FSKConfig{SampleRate: testRate, Baud: baud, DeviationHz: devHz, BT: 0.5})
	return m.Modulate(nil, prbs(int(float64(n)/testRate*baud)+8))[:n]
}

func afsk(n int) []complex64 {
	m := modem.NewAFSK(modem.AFSK1200(testRate))
	return m.Modulate(nil, prbs(int(float64(n)/testRate*1200)+2))[:n]
}

func ook(n int, baud float64) []complex64 {
	m := modem.NewOOK(modem.OOKConfig{SampleRate: testRate, Baud: baud})
	return m.Modulate(nil, prbs(int(float64(n)/testRate*baud)+2))[:n]
}

func cw(n int, wpm float64) []complex64 {
	m := modem.NewOOK(modem.OOKConfig{SampleRate: testRate, Baud: modem.CWBaud(wpm)})
	var out []complex64
	for len(out) < n {
		out = m.Modulate(out, modem.MorseLevels("CQ CQ DE R1ABC K "))
	}
	return out[:n]
}

func fmAudio(audio []float32, devHz float64) []complex64 {
	return modem.NewFM(modem.FMConfig{SampleRate: testRate, DeviationHz: devHz}).Modulate(nil, audio)
}

func tone(n int, hz float64) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(math.Sin(2 * math.Pi * hz * float64(i) / testRate))
	}
	return out
}

// voice — speech-like test signal: pitch harmonics with formant bumps, in syllables
// separated by short pauses. Analytic (one-sided) with positive frequencies; the real
// part is the audio. lowHz drops harmonics below it, as a voice filter does.
func voice(n int, lowHz float64, seed uint64) []complex64 {
	r := rand.New(rand.NewPCG(seed, seed^0x5bd1e995))
	out := make([]complex64, n)
	var phases [40]float64
	pos := 0
	for pos < n {
		on := int((0.12 + 0.13*r.Float64()) * testRate)
		off := int((0.03 + 0.06*r.Float64()) * testRate)
		f0 := 110 + 70*r.Float64()
		for i := 0; i < on && pos < n; i, pos = i+1, pos+1 {
			env := math.Sin(math.Pi * float64(i) / float64(on))
			pitch := f0 * (1 + 0.05*math.Sin(2*math.Pi*3*float64(i)/testRate))
			var s complex128
			for k := 1; k < len(phases); k++ {
				f := float64(k) * pitch
				phases[k] = math.Mod(phases[k]+2*math.Pi*f/testRate, 2*math.Pi)
				if f < lowHz || f > 3000 {
					continue
				}
				a := 1/float64(k) + 0.8*math.Exp(-sq((f-500)/200)) + 0.4*math.Exp(-sq((f-1500)/300))
				s += cmplx.Rect(a, phases[k])
			}
			out[pos] = complex64(s * complex(env, 0))
		}
		pos += off
	}
	return normalize(out)
}

func sq(x float64) float64 { return x * x }

// normalize scales to unit mean power: the channel sets SNR against power 1.
func normalize(x []complex64) []complex64 {
	var p float64
	for _, v := range x {
		p += float64(real(v)*real(v) + imag(v)*imag(v))
	}
	g := complex64(complex(math.Sqrt(float64(len(x))/p), 0))
	for i := range x {
		x[i] *= g
	}
	return x
}

func realPart(x []complex64) []float32 {
	out := make([]float32, len(x))
	var peak float32
	for i, v := range x {
		out[i] = real(v)
		peak = max(peak, abs32(real(v)))
	}
	for i := range out {
		out[i] /= peak
	}
	return out
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func conj(x []complex64) []complex64 {
	out := make([]complex64, len(x))
	for i, v := range x {
		out[i] = complex(real(v), -imag(v))
	}
	return out
}

// noisy adds white noise at snrDB against unit signal power within bwHz.
func noisy(x []complex64, snrDB, bwHz float64, seed uint64) []complex64 {
	return noisyRate(x, snrDB, bwHz, seed, testRate)
}

func noisyRate(x []complex64, snrDB, bwHz float64, seed uint64, rate float64) []complex64 {
	ch := modem.NewChannel(modem.ChannelConfig{
		SampleRate: rate, SNRdB: snrDB, NoiseBandwidthHz: bwHz, Seed: seed,
	})
	return ch.Process(nil, x)
}

type testSignal struct {
	name    string
	iq      []complex64
	noiseBW float64 // band in which SNR is set: about the occupied band
	want    string
}

func testSignals() []testSignal {
	half := int(testRate / 2)
	return []testSignal{
		{"carrier", carrier(half, 300), 500, ModCarrier},
		{"cw 18 wpm", cw(2*int(testRate), 18), 500, ModCW},
		{"ook 2000", ook(half, 2000), 8000, ModOOK},
		{"fm voice", fmAudio(realPart(voice(half, 0, 1)), 2500), 12_000, ModFMVoice},
		{"fm tone", fmAudio(tone(half, 1000), 3000), 10_000, ModFM},
		{"afsk 1200", afsk(half), 12_000, ModAFSK},
		{"gfsk 4800", gfsk(half, 4800, 2400), 12_000, Mod2FSK},
		{"gfsk 9600", gfsk(half, 9600, 3000), 18_000, Mod2FSK},
		{"usb voice", voice(half, 300, 2), 3000, ModUSB},
		{"lsb voice", conj(voice(half, 300, 3)), 3000, ModLSB},
	}
}

func TestClassify_EachModulationAtGoodSNR(t *testing.T) {
	for _, snr := range []float64{25, 15} {
		for i, s := range testSignals() {
			t.Run(fmt.Sprintf("%s/%.0fdB", s.name, snr), func(t *testing.T) {
				res := Classify(noisy(s.iq, snr, s.noiseBW, uint64(i+1)), testRate)
				if res.Modulation != s.want {
					t.Errorf("modulation %q, want %q (why: %s)", res.Modulation, s.want, res.Why)
				}
				if res.Confidence <= 0 || res.Confidence > 1 {
					t.Errorf("confidence %.2f, want (0, 1]", res.Confidence)
				}
				if res.Why == "" {
					t.Error("empty why")
				}
			})
		}
	}
}

// At low SNR the answer is "undetermined" or right, never another modulation. A signal
// whose power sits near its centre may still be above the threshold in its occupied band.
func TestClassify_NoWrongAnswerAtLowSNR(t *testing.T) {
	undetermined := 0
	for i, s := range testSignals() {
		t.Run(s.name, func(t *testing.T) {
			res := Classify(noisy(s.iq, 3, s.noiseBW, uint64(100+i)), testRate)
			switch res.Modulation {
			case ModUndetermined:
				undetermined++
			case s.want:
			default:
				t.Errorf("modulation %q at 3 dB, want %q or %q (why: %s)",
					res.Modulation, ModUndetermined, s.want, res.Why)
			}
		})
	}
	if undetermined < len(testSignals())/2 {
		t.Errorf("%d of %d undetermined at 3 dB, want most", undetermined, len(testSignals()))
	}
}

// Below the SNR threshold 2FSK is still named when its bits read: a slow stream has more
// energy per symbol than the SNR in its band shows.
func TestClassify_SlowFSKBelowSNRThreshold(t *testing.T) {
	for seed := uint64(1); seed <= 5; seed++ {
		res := Classify(noisy(gfsk(int(testRate/2), 4800, 2400), 5, 12_000, 200+seed), testRate)
		if res.Modulation != Mod2FSK || res.Params.Baud != 4800 {
			t.Errorf("seed %d: modulation %q at %.0f Bd, want %q at 4800 (why: %s)",
				seed, res.Modulation, res.Params.Baud, Mod2FSK, res.Why)
		}
	}
}

// Below the SNR threshold nothing but a real FSK stream is named 2FSK.
func TestClassify_NoFalseFSKBelowSNRThreshold(t *testing.T) {
	for i, s := range testSignals() {
		if s.want == Mod2FSK {
			continue
		}
		for _, snr := range []float64{0, 3, 5, 7, 9} {
			for seed := uint64(1); seed <= 3; seed++ {
				res := Classify(noisy(s.iq, snr, s.noiseBW, uint64(300+100*i)+seed*7+uint64(snr)), testRate)
				if res.Modulation == Mod2FSK {
					t.Errorf("%s at %.0f dB seed %d: 2fsk (why: %s)", s.name, snr, seed, res.Why)
				}
			}
		}
	}
}

func TestClassify_NoiseIsUndetermined(t *testing.T) {
	res := Classify(noisy(make([]complex64, int(testRate/2)), 0, testRate, 7), testRate)
	if res.Modulation != ModUndetermined {
		t.Errorf("modulation %q on noise, want %q", res.Modulation, ModUndetermined)
	}
}

func TestClassify_FSKBaudAndDeviation(t *testing.T) {
	res := Classify(noisy(gfsk(int(testRate/2), 4800, 2400), 20, 12_000, 11), testRate)
	if res.Params.Baud != 4800 {
		t.Errorf("baud %.0f, want 4800", res.Params.Baud)
	}
	// Gaussian shaping keeps short runs inside the full deviation.
	if d := res.Params.DeviationHz; d < 1700 || d > 2700 {
		t.Errorf("deviation %.0f Hz, want about 2400", d)
	}
	if w := res.Params.OccupiedHz; w < 6000 || w > 14_000 {
		t.Errorf("occupied %.0f Hz, want about 10 kHz", w)
	}
}

func TestClassify_AFSKBaud(t *testing.T) {
	res := Classify(noisy(afsk(int(testRate/2)), 20, 12_000, 12), testRate)
	if res.Params.Baud != 1200 {
		t.Errorf("baud %.0f, want 1200", res.Params.Baud)
	}
}

func TestClassify_OOKBaud(t *testing.T) {
	res := Classify(noisy(ook(int(testRate/2), 2000), 20, 8000, 13), testRate)
	if res.Params.Baud != 2000 {
		t.Errorf("baud %.0f, want 2000", res.Params.Baud)
	}
}

func TestClassify_CWSpeed(t *testing.T) {
	res := Classify(noisy(cw(2*int(testRate), 18), 20, 500, 14), testRate)
	if w := res.Params.WPM; w < 15 || w > 21 {
		t.Errorf("speed %.1f WPM, want about 18", w)
	}
	if w := res.Params.OccupiedHz; w >= 500 {
		t.Errorf("occupied %.0f Hz, want under 500", w)
	}
}

// lowpass keeps |f| < cutoffHz: a signal cut narrower than itself gets a noise-like envelope.
func lowpass(x []complex64, cutoffHz float64) []complex64 {
	taps := dsp.DesignLowpass(cutoffHz, testRate, 255, dsp.WindowHann)
	return dsp.NewXlatingFIR(0, testRate, 1, taps).Process(x)[len(taps):]
}

func gaussian(n int, seed uint64) []complex64 {
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b9))
	out := make([]complex64, n)
	for i := range out {
		out[i] = complex(float32(r.NormFloat64()), float32(r.NormFloat64()))
	}
	return out
}

// A noise-like envelope dips to zero all the time, far faster than any keying: not OOK.
func TestClassify_NoiseLikeEnvelopeIsNotKeyed(t *testing.T) {
	n := int(testRate / 2)
	cases := []struct {
		name string
		iq   []complex64
	}{
		{"band noise 10 kHz", normalize(lowpass(gaussian(n+300, 21), 5000))},
		{"gfsk 9600 cut to 6 kHz", normalize(lowpass(gfsk(n+300, 9600, 3000), 3000))},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := Classify(noisy(c.iq, 25, 10_000, uint64(30+i)), testRate)
			if res.Modulation == ModOOK || res.Modulation == ModCW {
				t.Errorf("modulation %q, want not keyed (why: %s)", res.Modulation, res.Why)
			}
		})
	}
}

// Band noise keeps its noise-like envelope after pauses are cut out: not a constant one.
func TestClassify_BandNoiseIsNotConstantEnvelope(t *testing.T) {
	n := int(testRate / 2)
	res := Classify(noisy(normalize(lowpass(gaussian(n+300, 22), 5000)), 25, 10_000, 32), testRate)
	if res.Modulation != ModUnknown {
		t.Errorf("modulation %q, want %q (why: %s)", res.Modulation, ModUnknown, res.Why)
	}
}

// packets — GFSK packets of onMs separated by offMs without a carrier.
func packets(n int, onMs, offMs float64) []complex64 {
	on, off := int(onMs*testRate/1000), int(offMs*testRate/1000)
	out := make([]complex64, 0, n+on)
	for len(out) < n {
		out = append(out, gfsk(on, 9600, 3000)...)
		out = append(out, make([]complex64, off)...)
	}
	return out[:n]
}

// A long transmission of packets with short pauses: the pauses are not envelope variation.
func TestClassify_PacketsWithShortPausesAre2FSK(t *testing.T) {
	res := Classify(noisy(packets(int(testRate/2), 64, 7), 20, 18_000, 17), testRate)
	if res.Modulation != Mod2FSK {
		t.Errorf("modulation %q, want %q (why: %s)", res.Modulation, Mod2FSK, res.Why)
	}
}

// One short packet in a slice padded for the detector delay: most of the slice is noise.
func TestClassify_ShortPacketInLongSliceIs2FSK(t *testing.T) {
	for _, sliceMs := range []float64{600, 2000} {
		t.Run(fmt.Sprintf("%.0fms", sliceMs), func(t *testing.T) {
			n := int(sliceMs * testRate / 1000)
			pkt := int(0.064 * testRate)
			x := make([]complex64, n)
			copy(x[(n-pkt)/2:], gfsk(pkt, 9600, 3000))
			res := Classify(noisy(x, 20, 18_000, 18), testRate)
			if res.Modulation != Mod2FSK {
				t.Errorf("modulation %q, want %q (why: %s)", res.Modulation, Mod2FSK, res.Why)
			}
		})
	}
}

func TestClassify_BurstWithSilenceAroundIsNotOOK(t *testing.T) {
	// A detector slice of a short packet: noise, then GFSK, then noise again.
	n := int(testRate / 2)
	x := make([]complex64, 0, n)
	x = append(x, make([]complex64, n/5)...)
	x = append(x, gfsk(3*n/5, 4800, 2400)...)
	x = append(x, make([]complex64, n/5)...)
	res := Classify(noisy(x, 20, 12_000, 15), testRate)
	if res.Modulation != Mod2FSK {
		t.Errorf("modulation %q, want %q (why: %s)", res.Modulation, Mod2FSK, res.Why)
	}
}

// sstvScanFM — IQ of Robot 36 scan lines (VIS skipped): FM of SSTV audio.
// False FSK clock lines appear here; the tree must still answer FM, not unknown.
func sstvScanFM(n int) []complex64 {
	enc := sstv.NewEncoder(sstv.TestPattern(320, 240), sstv.Robot36, testRate)
	skip := make([]float32, int(1.2*testRate))
	enc.Read(skip)
	audio := make([]float32, n)
	got := enc.Read(audio)
	if got < n {
		for i := got; i < n; i++ {
			audio[i] = 0
		}
	}
	return fmAudio(audio, 5000)
}

func TestClassify_SSTVScanLinesAreFM(t *testing.T) {
	res := Classify(noisy(sstvScanFM(int(testRate/2)), 20, 18_000, 41), testRate)
	if res.Modulation != ModFM {
		t.Fatalf("modulation %q, want %q (why: %s)", res.Modulation, ModFM, res.Why)
	}
	if res.Service != sstvProbable {
		t.Fatalf("service %q, want %q (label %q)", res.Service, sstvProbable, res.ServiceLabel)
	}
}

// sstvWithVIS — IQ from the start of a Robot 36 transmission (VIS + first lines).
func sstvWithVIS(n int) []complex64 {
	enc := sstv.NewEncoder(sstv.TestPattern(320, 240), sstv.Robot36, testRate)
	audio := make([]float32, n)
	enc.Read(audio)
	return fmAudio(audio, 5000)
}

func TestClassify_SSTVVISSetsService(t *testing.T) {
	res := Classify(noisy(sstvWithVIS(int(1.5*testRate)), 20, 18_000, 42), testRate)
	if res.Service != sstvService {
		t.Fatalf("service %q label %q mod %q why %q, want %q",
			res.Service, res.ServiceLabel, res.Modulation, res.Why, sstvService)
	}
	if res.Modulation != ModFM {
		t.Errorf("modulation %q, want %q", res.Modulation, ModFM)
	}
	if res.ServiceLabel != "SSTV, Robot 36" {
		t.Errorf("label %q, want SSTV, Robot 36", res.ServiceLabel)
	}
}

func TestClassify_PlainFMToneHasNoSSTVService(t *testing.T) {
	res := Classify(noisy(fmAudio(tone(int(testRate/2), 1000), 3000), 20, 10_000, 43), testRate)
	if res.Service != "" {
		t.Fatalf("service %q on a plain tone, want empty (mod %q)", res.Service, res.Modulation)
	}
}

// PD120 scan is slow: a 1.2 s slice looks like one RF line (carrier). It must still
// be SSTV by the 1200–2300 Hz tones after the FM detector, not a bare carrier.
func TestClassify_PD120ScanLinesAreSSTVNotCarrier(t *testing.T) {
	enc := sstv.NewEncoder(sstv.TestPattern(640, 496), sstv.PD120, testRate)
	skip := make([]float32, int(2.0*testRate)) // past VIS into slow scan
	enc.Read(skip)
	audio := make([]float32, int(testRate*1.2))
	enc.Read(audio)
	res := Classify(noisy(fmAudio(audio, 5000), 20, 18_000, 44), testRate)
	if res.Modulation == ModCarrier && res.Service == "" {
		t.Fatalf("modulation %q service %q why %q, want FM/SSTV not a bare carrier",
			res.Modulation, res.Service, res.Why)
	}
	if res.Service != sstvProbable && res.Service != sstvService {
		t.Fatalf("service %q, want sstv or sstv_probable (mod %q why %q)",
			res.Service, res.Modulation, res.Why)
	}
}

// Unmodulated carrier must stay carrier: no SSTV false alarm on a CW line.
func TestClassify_TrueCarrierHasNoSSTVService(t *testing.T) {
	res := Classify(noisy(carrier(int(testRate/2), 0), 25, 500, 45), testRate)
	if res.Modulation != ModCarrier {
		t.Fatalf("modulation %q, want carrier (why %s)", res.Modulation, res.Why)
	}
	if res.Service != "" {
		t.Fatalf("service %q on a true carrier", res.Service)
	}
}

// wfmTestRate — частота отсчётов для широкой ЧМ: ±75 кГц девиации не влезает в 48 кГц.
const wfmTestRate = 512_000.0

// broadcastWFM is ~0.5 s of wide FM: random audio at 15 kHz, ±75 kHz deviation.
func broadcastWFM(n int, pilot float64) []complex64 {
	rng := rand.New(rand.NewPCG(9, 10))
	step := wfmTestRate / 15_000
	audio := make([]float32, n)
	a, b := rng.Float64()*2-1, rng.Float64()*2-1
	for i := range audio {
		k := math.Mod(float64(i), step) / step
		if i > 0 && math.Mod(float64(i), step) < math.Mod(float64(i-1), step) {
			a, b = b, rng.Float64()*2-1
		}
		v := a + (b-a)*k
		if pilot != 0 {
			v = 0.85*v + pilot*math.Sin(2*math.Pi*19_000*float64(i)/wfmTestRate)
		}
		audio[i] = float32(v)
	}
	return modem.NewFM(modem.FMConfig{SampleRate: wfmTestRate, DeviationHz: 75_000}).Modulate(nil, audio)
}

// fading multiplies IQ by a slow amplitude swing so κ of a constant envelope rises.
func fading(x []complex64, depth, hz, rate float64) []complex64 {
	out := make([]complex64, len(x))
	for i, v := range x {
		g := 1 + depth*math.Cos(2*math.Pi*hz*float64(i)/rate)
		out[i] = complex(real(v)*float32(g), imag(v)*float32(g))
	}
	return out
}

func TestClassify_WideFMAtGoodSNR(t *testing.T) {
	res := Classify(noisyRate(broadcastWFM(int(wfmTestRate/2), 0), 25, 180_000, 51, wfmTestRate), wfmTestRate)
	if res.Modulation != ModWFM {
		t.Fatalf("modulation %q, want %q (why: %s)", res.Modulation, ModWFM, res.Why)
	}
}

func TestClassify_WideFMWithFadingStillWFM(t *testing.T) {
	iq := fading(broadcastWFM(int(wfmTestRate/2), 0), 0.35, 7, wfmTestRate)
	res := ClassifyHint(noisyRate(iq, 25, 180_000, 52, wfmTestRate), wfmTestRate, 180_000)
	if res.Modulation != ModWFM {
		t.Fatalf("modulation %q, want %q (why: %s)", res.Modulation, ModWFM, res.Why)
	}
}

func TestClassify_LFMChirp(t *testing.T) {
	const rate = 500_000.0
	sw := modem.NewSweep(rate, 125_000, 7)
	iq := make([]complex64, int(rate*0.12))
	for i := range iq {
		iq[i] = sw.Next()
	}
	res := Classify(noisyRate(iq, 25, 160_000, 90, rate), rate)
	if res.Modulation != ModLFM {
		t.Fatalf("modulation %q, want %q (why: %s)", res.Modulation, ModLFM, res.Why)
	}
	if rel := math.Abs(res.Params.ChirpHzS-sw.SlopeHzS()) / sw.SlopeHzS(); rel > 0.25 {
		t.Fatalf("slope %.0f, want about %.0f", res.Params.ChirpHzS, sw.SlopeHzS())
	}
	if math.Abs(res.Params.OccupiedHz-sw.BandwidthHz()) > 40_000 {
		t.Fatalf("occupied %.0f, want about %.0f", res.Params.OccupiedHz, sw.BandwidthHz())
	}
}

func TestClassify_WideGMSKIsNotWideFM(t *testing.T) {
	const rate = 1_024_000.0
	n := int(rate / 5)
	m := modem.NewFSK(modem.FSKConfig{SampleRate: rate, Baud: 270833, DeviationHz: 67708, BT: 0.3})
	iq := m.Modulate(nil, prbs(int(float64(n)/rate*270833)+16))
	if len(iq) > n {
		iq = iq[:n]
	}
	res := Classify(noisyRate(iq, 25, 250_000, 91, rate), rate)
	if res.Modulation != Mod2FSK {
		t.Fatalf("modulation %q, want %q (why: %s, occupied %.0f, baud %.0f)",
			res.Modulation, Mod2FSK, res.Why, res.Params.OccupiedHz, res.Params.Baud)
	}
}

func TestClassify_WideFMPilotSetsBroadcast(t *testing.T) {
	res := Classify(noisyRate(broadcastWFM(int(wfmTestRate/2), 0.15), 25, 180_000, 53, wfmTestRate), wfmTestRate)
	if res.Modulation != ModWFM {
		t.Fatalf("modulation %q, want %q (why: %s)", res.Modulation, ModWFM, res.Why)
	}
	if res.Service != ServiceBroadcast || res.ServiceLabel != LabelBroadcast {
		t.Fatalf("service %q/%q, want broadcast/%s", res.Service, res.ServiceLabel, LabelBroadcast)
	}
}

func TestTagBroadcast_BandPlanWithoutPilot(t *testing.T) {
	r := Result{Modulation: ModWFM, Why: "wide"}
	got := TagBroadcast(r, 104_100_000)
	if got.Service != ServiceBroadcast || got.ServiceLabel != LabelBroadcast || got.ServiceBasis != BasisSignature {
		t.Fatalf("got %+v, want broadcast on the FM band", got)
	}
	if TagBroadcast(r, 145_900_000).Service != "" {
		t.Fatal("tagged outside the broadcast band")
	}
}
