package modem

import (
	"math"
	"math/rand/v2"
	"time"
)

// ChannelConfig — параметры радиоканала имитатора.
type ChannelConfig struct {
	SampleRate float64 // частота дискретизации IQ, Гц

	// OffsetHz — сдвиг сигнала от центра приёмника в момент t от начала потока:
	// расстройка передатчика плюс Доплер. nil — без сдвига.
	OffsetHz func(t time.Duration) float64

	// SNRdB — отношение мощности сигнала (принимается равной 1) к мощности шума
	// в полосе NoiseBandwidthHz. Шум белый, на всю полосу дискретизации.
	SNRdB float64
	// NoiseBandwidthHz — полоса, в которой задан SNR (обычно полоса канала). 0 — без шума.
	NoiseBandwidthHz float64

	// Seed — зерно генератора шума: одинаковое зерно даёт одинаковый шум.
	Seed uint64
}

// Channel переносит сигнал по частоте и добавляет белый гауссов шум.
// Фаза и время сохраняются между вызовами Process.
type Channel struct {
	cfg      ChannelConfig
	noiseStd float64 // СКО шума на одну компоненту (I или Q)
	rng      *rand.Rand
	phase    float64
	n        int // отсчётов обработано с начала
}

// NewChannel создаёт канал.
func NewChannel(cfg ChannelConfig) *Channel {
	c := &Channel{
		cfg: cfg,
		rng: rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9E3779B97F4A7C15)),
	}
	if cfg.NoiseBandwidthHz > 0 {
		// Мощность шума в полосе B равна 1/SNR, значит во всей полосе fs — (1/SNR)·fs/B.
		total := math.Pow(10, -cfg.SNRdB/10) * cfg.SampleRate / cfg.NoiseBandwidthHz
		c.noiseStd = math.Sqrt(total / 2)
	}
	return c
}

// Process дописывает в dst обработанные отсчёты src и возвращает расширенный срез.
func (c *Channel) Process(dst, src []complex64) []complex64 {
	for _, s := range src {
		if c.cfg.OffsetHz != nil {
			t := time.Duration(float64(c.n) / c.cfg.SampleRate * float64(time.Second))
			c.phase = math.Mod(c.phase+twoPi*c.cfg.OffsetHz(t)/c.cfg.SampleRate, twoPi)
		}
		c.n++

		y := complex128(s)
		if c.phase != 0 {
			sin, cos := math.Sincos(c.phase)
			y *= complex(cos, sin)
		}
		if c.noiseStd > 0 {
			y += complex(c.rng.NormFloat64()*c.noiseStd, c.rng.NormFloat64()*c.noiseStd)
		}
		dst = append(dst, complex64(y))
	}
	return dst
}
