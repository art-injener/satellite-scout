package modem

import (
	"math/cmplx"
	"slices"
	"testing"
)

const ookTestRate = 48_000.0

func TestOOK_LevelsGiveCarrierAndSilence(t *testing.T) {
	m := NewOOK(OOKConfig{SampleRate: ookTestRate, Baud: 1000})
	iq := m.Modulate(nil, []byte{1, 0, 1})
	sps := int(ookTestRate / 1000)
	if len(iq) != 3*sps {
		t.Fatalf("%d samples, want %d", len(iq), 3*sps)
	}
	// Middle of each symbol: past the edge ramp.
	if a := cmplx.Abs(complex128(iq[sps/2])); a < 0.99 || a > 1.01 {
		t.Errorf("mark amplitude %.3f, want 1", a)
	}
	if a := cmplx.Abs(complex128(iq[sps+sps/2])); a > 1e-6 {
		t.Errorf("space amplitude %.3f, want 0", a)
	}
}

func TestOOK_EdgesAreRamped(t *testing.T) {
	m := NewOOK(OOKConfig{SampleRate: ookTestRate, Baud: 1000})
	iq := m.Modulate(nil, []byte{0, 1})
	sps := int(ookTestRate / 1000)
	// The first samples of the mark rise gradually, not in one step.
	first := cmplx.Abs(complex128(iq[sps]))
	if first > 0.2 {
		t.Errorf("first sample of the mark %.3f, want a ramp from 0", first)
	}
}

func TestOOK_SplitBlocksMatchOneCall(t *testing.T) {
	levels := []byte{1, 1, 0, 1, 0, 0, 1, 0, 1, 1}
	whole := NewOOK(OOKConfig{SampleRate: ookTestRate, Baud: 1200}).Modulate(nil, levels)
	m := NewOOK(OOKConfig{SampleRate: ookTestRate, Baud: 1200})
	parts := m.Modulate(nil, levels[:3])
	parts = m.Modulate(parts, levels[3:])
	if !slices.Equal(whole, parts) {
		t.Error("split modulation differs from a single call")
	}
}

func TestMorseLevels_UnitsOfDotDashAndGaps(t *testing.T) {
	// «ET»: dot, letter gap of 3, dash of 3.
	got := MorseLevels("ET")
	want := []byte{1, 0, 0, 0, 1, 1, 1}
	if !slices.Equal(got, want) {
		t.Errorf("MorseLevels(ET) = %v, want %v", got, want)
	}
}

func TestMorseLevels_WordGapIsSevenUnits(t *testing.T) {
	got := MorseLevels("E E")
	want := []byte{1, 0, 0, 0, 0, 0, 0, 0, 1}
	if !slices.Equal(got, want) {
		t.Errorf("MorseLevels(E E) = %v, want %v", got, want)
	}
}

func TestMorseLevels_UnknownCharactersSkipped(t *testing.T) {
	if got := MorseLevels("e#"); !slices.Equal(got, []byte{1}) {
		t.Errorf("MorseLevels(e#) = %v, want [1]", got)
	}
}

func TestCWBaud_DotLengthFromWPM(t *testing.T) {
	// PARIS standard: 20 WPM is a 60 ms dot.
	if b := CWBaud(20); b < 16.6 || b > 16.7 {
		t.Errorf("CWBaud(20) = %.3f, want 16.67", b)
	}
}
