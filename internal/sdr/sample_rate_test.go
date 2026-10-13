package sdr

import (
	"errors"
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
