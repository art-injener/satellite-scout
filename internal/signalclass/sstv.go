package signalclass

import (
	"github.com/art-injener/satellite-scout/internal/modem"
	"github.com/art-injener/satellite-scout/internal/sstv"
)

const (
	// sstvService — служба по заголовку VIS.
	sstvService = "sstv"
	// sstvProbable — тоны яркости SSTV без подтверждённого VIS.
	sstvProbable = "sstv_probable"
	// sstvProbeDevHz — девиация пробного ЧМ: как у профиля SSTV.
	sstvProbeDevHz = 5000
	// sstvAudioRate — частота звука для декодера VIS.
	sstvAudioRate = 12_000
)

// withSSTV дополняет ответ службой SSTV: VIS даёт уверенный sstv, тоны без VIS — sstv_probable.
func withSSTV(a *analysis, r Result) Result {
	if a == nil || len(a.on) < int(a.rate/4) {
		return r
	}
	if r.Modulation != ModFM && r.Modulation != ModUnknown && r.Modulation != ModCarrier {
		return r
	}
	d := instFreq(a.on, a.rate)
	share := sstvToneShare(d, a.rate)
	if (r.Modulation == ModUnknown || r.Modulation == ModCarrier) && share < sstvToneMinShare {
		return r
	}
	if m, ok := probeVIS(a.on, a.rate); ok {
		r.Service = sstvService
		r.ServiceLabel = "SSTV, " + m.Name
		if r.Modulation == ModUnknown || r.Modulation == ModCarrier {
			r.Modulation = ModFM
			r.Why = "огибающая постоянна, заголовок VIS " + m.Name
		} else if r.Why != "" {
			r.Why += ", VIS " + m.Name
		}
		return r
	}
	if share >= sstvToneMinShare {
		if r.Modulation == ModCarrier {
			r.Modulation = ModFM
			r.Why = "огибающая постоянна, тоны в полосе SSTV (линия в спектре — медленная развёртка)"
		}
		r.Service = sstvProbable
		r.ServiceLabel = "SSTV?"
	}
	return r
}

// probeVIS — есть ли в отрезке заголовок VIS после ЧМ-демодуляции.
func probeVIS(iq []complex64, rate float64) (sstv.Mode, bool) {
	fm := modem.NewFMAudioDemod(modem.FMAudioConfig{
		SampleRate: rate, DeviationHz: sstvProbeDevHz, AudioRate: sstvAudioRate,
	})
	audio := fm.Demodulate(nil, iq)
	if len(audio) < int(sstvAudioRate) {
		return sstv.Mode{}, false
	}
	dec := sstv.NewDecoder(sstvAudioRate)
	for _, e := range dec.Push(audio) {
		if e.Kind == sstv.EventStart {
			return e.Mode, true
		}
	}
	return sstv.Mode{}, false
}
