package modem

import (
	"math"
	"math/cmplx"
)

// Sweep — повторяющаяся ЛЧМ: несколько восходящих свипов и два нисходящих,
// как преамбула LoRa. Фаза между вызовами Next сохраняется.
type Sweep struct {
	rate, bw float64
	absSlope float64
	slope    float64
	freq     float64
	phase    float64
	nSym     int
	i        int
	left     int
	upN      int
	downN    int
	down     bool
}

// NewSweep строит генератор. bandwidthHz шире 0,4 частоты отсчётов сужается,
// чтобы свип поместился в полосу. sf вне 5…12 заменяется на 7.
func NewSweep(sampleRate, bandwidthHz, sf float64) *Sweep {
	if sampleRate <= 0 {
		sampleRate = 48_000
	}
	if sf < 5 || sf > 12 {
		sf = 7
	}
	if bandwidthHz <= 0 {
		bandwidthHz = 125_000
	}
	if maxBW := 0.4 * sampleRate; bandwidthHz > maxBW {
		bandwidthHz = maxBW
	}
	nSym := max(8, int(math.Round(sampleRate*math.Pow(2, sf)/bandwidthHz)))
	abs := bandwidthHz * sampleRate / float64(nSym)
	return &Sweep{
		rate: sampleRate, bw: bandwidthHz, absSlope: abs, slope: abs,
		freq: -bandwidthHz / 2, nSym: nSym, left: 8, upN: 8, downN: 2,
	}
}

// BandwidthHz — размах одного свипа, Гц.
func (s *Sweep) BandwidthHz() float64 { return s.bw }

// SlopeHzS — наклон восходящего свипа, Гц/с. Нисходящий имеет обратный знак.
func (s *Sweep) SlopeHzS() float64 { return s.absSlope }

// SymbolSamples — отсчётов в одном символе.
func (s *Sweep) SymbolSamples() int { return s.nSym }

// Next — следующий отсчёт комплексной огибающей.
func (s *Sweep) Next() complex64 {
	v := complex64(cmplx.Rect(1, s.phase))
	s.phase += 2 * math.Pi * s.freq / s.rate
	s.phase = math.Mod(s.phase, 2*math.Pi)
	if s.phase < 0 {
		s.phase += 2 * math.Pi
	}
	s.freq += s.slope / s.rate
	s.i++
	if s.i >= s.nSym {
		s.i = 0
		s.advance()
	}
	return v
}

func (s *Sweep) advance() {
	s.left--
	if s.down {
		if s.left <= 0 {
			s.down = false
			s.left = s.upN
			s.freq = -s.bw / 2
			s.slope = s.absSlope
			return
		}
		s.freq = s.bw / 2
		s.slope = -s.absSlope
		return
	}
	if s.left <= 0 {
		s.down = true
		s.left = s.downN
		s.freq = s.bw / 2
		s.slope = -s.absSlope
		return
	}
	s.freq = -s.bw / 2
	s.slope = s.absSlope
}
