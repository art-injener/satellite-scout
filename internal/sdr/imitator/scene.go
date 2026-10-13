package imitator

import (
	"math"
	"math/rand/v2"
)

// scene — эфир имитатора: сумма сигналов плюс общий белый шум приёмника.
// Не потокобезопасна: работает в горутине потока.
type scene struct {
	sources  []*signalSource
	buf      []complex64
	noiseStd float64 // СКО шума на одну компоненту (I или Q)
	rng      *rand.Rand
}

func newScene(opts Options, sampleRate float64) (*scene, error) {
	s := &scene{rng: rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9E3779B97F4A7C15))}
	for _, sig := range opts.Signals {
		src, err := newSignalSource(sig, sampleRate)
		if err != nil {
			return nil, err
		}
		s.sources = append(s.sources, src)
	}
	if opts.NoiseDBFS != 0 {
		s.noiseStd = math.Sqrt(math.Pow(10, opts.NoiseDBFS/10) / 2)
	}
	return s, nil
}

// fill заполняет dst эфиром для приёмника, настроенного на centerHz.
func (s *scene) fill(dst []complex64, centerHz uint64) {
	clear(dst)
	if cap(s.buf) < len(dst) {
		s.buf = make([]complex64, len(dst))
	}
	buf := s.buf[:len(dst)]
	for _, src := range s.sources {
		src.fill(buf, centerHz)
		for i := range dst {
			dst[i] += buf[i]
		}
	}
	if s.noiseStd > 0 {
		for i := range dst {
			dst[i] += complex(float32(s.rng.NormFloat64()*s.noiseStd), float32(s.rng.NormFloat64()*s.noiseStd))
		}
	}
}
