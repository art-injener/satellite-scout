package sdr

import (
	"errors"
	"slices"
	"testing"
)

// rtlLikeCaps — диапазоны как у RTL-SDR: два участка с провалом между ними.
func rtlLikeCaps() Capabilities {
	return Capabilities{
		SampleRateRanges: []SampleRateRange{
			{MinHz: 225_001, MaxHz: 300_000},
			{MinHz: 900_001, MaxHz: 3_200_000},
		},
		RecommendedSampleRate: 2_048_000,
	}
}

func TestResolveSampleRate_NotConfiguredUsesRecommended(t *testing.T) {
	got, err := ResolveSampleRate(rtlLikeCaps(), 0)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != 2_048_000 {
		t.Errorf("rate=%d, want 2048000", got)
	}
}

func TestResolveSampleRate_ConfiguredWithinRange(t *testing.T) {
	got, err := ResolveSampleRate(rtlLikeCaps(), 1_024_000)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if got != 1_024_000 {
		t.Errorf("rate=%d, want 1024000", got)
	}
}

func TestResolveSampleRate_RangeBoundsInclusive(t *testing.T) {
	for _, rate := range []uint32{225_001, 300_000, 900_001, 3_200_000} {
		if _, err := ResolveSampleRate(rtlLikeCaps(), rate); err != nil {
			t.Errorf("rate %d: err=%v, want nil", rate, err)
		}
	}
}

func TestResolveSampleRate_ConfiguredUnsupported(t *testing.T) {
	for _, rate := range []uint32{100_000, 500_000, 3_200_001} {
		if _, err := ResolveSampleRate(rtlLikeCaps(), rate); !errors.Is(err, ErrSampleRateUnsupported) {
			t.Errorf("rate %d: err=%v, want ErrSampleRateUnsupported", rate, err)
		}
	}
}

func TestResolveSampleRate_DriverWithoutRecommendation(t *testing.T) {
	caps := rtlLikeCaps()
	caps.RecommendedSampleRate = 0
	if _, err := ResolveSampleRate(caps, 0); !errors.Is(err, ErrSampleRateUnknown) {
		t.Errorf("err=%v, want ErrSampleRateUnknown", err)
	}
}

func TestResolveSampleRate_DriverWithoutRangesAcceptsNothingConfigured(t *testing.T) {
	// Драйвер не сообщил допустимых значений: проверить заданную частоту нечем.
	caps := Capabilities{RecommendedSampleRate: 2_048_000}
	if _, err := ResolveSampleRate(caps, 1_024_000); !errors.Is(err, ErrSampleRateUnsupported) {
		t.Errorf("err=%v, want ErrSampleRateUnsupported", err)
	}
}

func TestCapabilities_UsableBandHz(t *testing.T) {
	if got := rtlLikeCaps().UsableBandHz(2_048_000); got != 2_048_000 {
		t.Errorf("no fraction: usable %d, want the whole 2048000", got)
	}
	caps := rtlLikeCaps()
	caps.UsableFraction = 0.8
	if got := caps.UsableBandHz(2_560_000); got != 2_048_000 {
		t.Errorf("fraction 0.8 of 2560000: usable %d, want 2048000", got)
	}
}

func TestSampleRateChoices(t *testing.T) {
	wide := Capabilities{SampleRateRanges: []SampleRateRange{{MinHz: 48_000, MaxHz: 2_400_000}}}
	tests := []struct {
		name string
		caps Capabilities
		base uint32
		want []uint32
	}{
		{"wide driver keeps every step", wide, 2_048_000, []uint32{2_048_000, 1_024_000, 512_000, 256_000}},
		{"rtl gap drops 512 kHz", rtlLikeCaps(), 2_048_000, []uint32{2_048_000, 1_024_000, 256_000}},
		{"rtl at 2.4 MHz drops 600 kHz", rtlLikeCaps(), 2_400_000, []uint32{2_400_000, 1_200_000, 300_000}},
		{"rtl at 2.56 MHz: no eighth, a tenth instead", rtlLikeCaps(), 2_560_000, []uint32{2_560_000, 1_280_000, 256_000}},
		{"steps below the driver minimum dropped", wide, 192_000, []uint32{192_000, 96_000, 48_000}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SampleRateChoices(tc.caps, tc.base)
			if !slices.Equal(got, tc.want) {
				t.Errorf("SampleRateChoices=%v, want %v", got, tc.want)
			}
		})
	}
}
