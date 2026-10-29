package rtlsdr

import (
	"context"
	"errors"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

const (
	probeSamples = 256
	probeTimeout = 2 * time.Second
)

// Probe открывает донгл, запускает его на частоте по умолчанию и читает немного отсчётов.
// Если донгл уже ведёт приём, захват интерфейса не удаётся и это видно в ошибке.
func Probe(serial, devicePath string) (sdr.ProbeReport, error) {
	return probe(New(Options{Serial: serial, DevicePath: devicePath}))
}

func probe(r *Receiver) (sdr.ProbeReport, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	ch, err := r.StartStream(ctx, sdr.StreamConfig{SampleRate: RecommendedSampleRate, BlockSize: probeSamples})
	if err != nil {
		return sdr.ProbeReport{}, err
	}
	defer func() { _ = r.Stop() }()

	var n int
	for n < probeSamples {
		f, ok := <-ch
		if !ok {
			return sdr.ProbeReport{}, errors.New("rtlsdr: no samples from the dongle")
		}
		n += len(f.Samples)
	}
	st := r.Status()
	caps := r.Capabilities()
	return sdr.ProbeReport{
		Tuner:      "R820T/R820T2",
		MinFreqHz:  caps.MinFreqHz,
		MaxFreqHz:  caps.MaxFreqHz,
		CenterHz:   st.CenterHz,
		SampleRate: st.SampleRate,
		GainDB:     st.GainDB,
		Samples:    n,
	}, nil
}
