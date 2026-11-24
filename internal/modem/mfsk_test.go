package modem

import (
	"testing"
)

func mfskPRBS(cfg MFSKConfig, n int) ([]byte, []complex64) {
	levels := make([]byte, n)
	p := NewPRBS9()
	mod := byte(cfg.Levels)
	for i := range levels {
		// Два бита PRBS → уровень 0…3; для 3FSK берём mod 3.
		v := p.Next()<<1 | p.Next()
		levels[i] = v % mod
	}
	return levels, NewMFSK(cfg).Modulate(nil, levels)
}

func checkMFSKLevels(t *testing.T, want, got []byte, settle int) {
	t.Helper()
	if len(got) < len(want)-3 {
		t.Fatalf("got %d symbols, want about %d", len(got), len(want))
	}
	// Выравнивание по сдвигу тактов ±2 символа.
	bestErr := len(want)
	for off := 0; off <= 4; off++ {
		err := 0
		n := 0
		for i := settle; i < len(want) && i+off < len(got); i++ {
			n++
			if got[i+off] != want[i] {
				err++
			}
		}
		if n > 0 && err < bestErr {
			bestErr = err
		}
	}
	if bestErr != 0 {
		t.Errorf("symbol errors=%d after settle %d", bestErr, settle)
	}
}

func TestMFSKDemod_FSK4DMRClean(t *testing.T) {
	cfg := FSK4DMR4800(fskTestRate)
	want, iq := mfskPRBS(cfg, 8000)
	got := NewMFSKDemod(cfg).Demodulate(nil, iq)
	checkMFSKLevels(t, want, got, 64)
}

func TestMFSKDemod_FSK3Clean(t *testing.T) {
	cfg := FSK3_4800(fskTestRate)
	want, iq := mfskPRBS(cfg, 8000)
	got := NewMFSKDemod(cfg).Demodulate(nil, iq)
	checkMFSKLevels(t, want, got, 64)
}

func TestMFSKDemod_GMFSKClean(t *testing.T) {
	cfg := GMFSK4800(fskTestRate)
	want, iq := mfskPRBS(cfg, 8000)
	got := NewMFSKDemod(cfg).Demodulate(nil, iq)
	checkMFSKLevels(t, want, got, 64)
}