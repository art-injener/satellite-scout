package sdr

import (
	"context"
	"math"
)

// DefaultBlockSize — количество IQ-отсчётов в одном фрейме по умолчанию.
// При 2.4 MSPS даёт ~6.8 мс на фрейм — баланс между latency и overhead.
const DefaultBlockSize = 16384

// IQFrame — один блок IQ-отсчётов от приёмника.
type IQFrame struct {
	Samples     []complex64
	CenterHz    uint64
	SampleRate  uint32
	TimestampMs uint64 // миллисекунды с начала эпохи Unix
}

// StreamConfig — параметры запуска IQ-потока.
type StreamConfig struct {
	CenterHz   uint64
	SampleRate uint32
	GainDB     float32 // отрицательное значение = автоусиление
	BlockSize  int     // 0 = DefaultBlockSize
}

// EffectiveBlockSize возвращает BlockSize или DefaultBlockSize при нулевом значении.
func (c StreamConfig) EffectiveBlockSize() int {
	if c.BlockSize > 0 {
		return c.BlockSize
	}
	return DefaultBlockSize
}

// Capabilities — возможности приёмника.
type Capabilities struct {
	MinFreqHz        uint64
	MaxFreqHz        uint64
	MaxBandwidthHz   uint64
	SupportsParallel bool

	// SampleRateRanges — допустимые частоты дискретизации (у RTL-SDR два участка).
	SampleRateRanges []SampleRateRange
	// RecommendedSampleRate — частота, если в конфиге тракта не задана.
	RecommendedSampleRate uint32
	// UsableFraction — доля окна по центру, которую показывают и где ставят канал: у краёв
	// фильтр приёмника заваливает полосу и туда же складываются зеркала из-за Найквиста.
	// 0 — годно всё окно.
	UsableFraction float64
}

// UsableBandHz — ширина годной середины окна при частоте дискретизации rate.
func (c Capabilities) UsableBandHz(rate uint32) uint32 {
	if c.UsableFraction <= 0 || c.UsableFraction >= 1 {
		return rate
	}
	return uint32(math.Round(float64(rate) * c.UsableFraction))
}

// ReceiverStatus — текущее состояние приёмника (для GET /api/sdr/status).
type ReceiverStatus struct {
	CenterHz   uint64  `json:"center_hz"`
	SampleRate uint32  `json:"sample_rate"`
	GainDB     float32 `json:"gain_db"`
	Running    bool    `json:"running"`
}

// Receiver — интерфейс SDR-устройства (ADR-004 §2.3).
type Receiver interface {
	Name() string
	Capabilities() Capabilities
	Tune(frequencyHz uint64) error
	SetGain(db float32) error
	Status() ReceiverStatus
	StartStream(ctx context.Context, cfg StreamConfig) (<-chan IQFrame, error)
	Stop() error
}
