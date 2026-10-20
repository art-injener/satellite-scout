package sdr

import (
	"errors"
	"fmt"
)

var (
	ErrSampleRateUnsupported = errors.New("sample rate not supported by driver")
	ErrSampleRateUnknown     = errors.New("sample rate not configured and driver has no recommended value")
	// ErrSampleRateRequired — поток запрошен без частоты дискретизации; драйвер её не подставляет.
	ErrSampleRateRequired = errors.New("stream sample rate is required")
)

// SampleRateRange — допустимый участок частот дискретизации драйвера, границы включены.
type SampleRateRange struct {
	MinHz uint32
	MaxHz uint32
}

// ResolveSampleRate выбирает частоту дискретизации тракта: заданную в конфиге
// (0 — не задана) или рекомендуемую драйвером. Заданная проверяется по диапазонам драйвера.
func ResolveSampleRate(caps Capabilities, configured uint32) (uint32, error) {
	if configured == 0 {
		if caps.RecommendedSampleRate == 0 {
			return 0, ErrSampleRateUnknown
		}
		return caps.RecommendedSampleRate, nil
	}
	for _, r := range caps.SampleRateRanges {
		if configured >= r.MinHz && configured <= r.MaxHz {
			return configured, nil
		}
	}
	return 0, fmt.Errorf("%w: %d Hz", ErrSampleRateUnsupported, configured)
}

// sampleRateDivisors — ступени «Полосы»: вся базовая частота, половина, четверть и восьмая.
var sampleRateDivisors = []uint32{1, 2, 4, 8}

// SampleRateChoices — частоты, между которыми оператор переключает «Полосу»: базовая,
// делённая на ступени, без тех, что драйвер не поддерживает (у RTL-SDR — провал 300–900 кГц).
func SampleRateChoices(caps Capabilities, base uint32) []uint32 {
	var out []uint32
	for _, d := range sampleRateDivisors {
		// Ноль ResolveSampleRate понял бы как «не задано» и вернул бы рекомендуемую
		if base/d == 0 {
			continue
		}
		if rate, err := ResolveSampleRate(caps, base/d); err == nil {
			out = append(out, rate)
		}
	}
	return out
}
