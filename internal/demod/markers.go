package demod

import (
	"math"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
)

// ExpectedTransmitter — передатчик КА, который может звучать в полосе приёмника.
type ExpectedTransmitter struct {
	NoradID int
	UUID    string
	FreqHz  int64 // номинальный downlink
}

// TransmitterSource — передатчики КА группы сопровождения (План сеансов и SatNOGS).
type TransmitterSource interface {
	ExpectedTransmitters() []ExpectedTransmitter
}

// TxMarker — передатчик в окне приёмника: где он звучит сейчас с учётом Доплера.
// Номинал — FreqHz минус DopplerHz.
type TxMarker struct {
	NoradID   int     `json:"norad_id"`
	UUID      string  `json:"uuid"`
	FreqHz    int64   `json:"freq_hz"`
	DopplerHz float64 `json:"doppler_hz"`
}

// WithTransmitters включает маркеры передатчиков в статусе демодулятора.
func (s *Service) WithTransmitters(src TransmitterSource) *Service {
	s.transmitters = src
	return s
}

// bandMarkers — маркеры для окна кадра; nil, если источника передатчиков или орбит нет.
func (s *Service) bandMarkers(centerHz uint64, spanHz uint32) []TxMarker {
	if s.transmitters == nil {
		return nil
	}
	return MarkersInBand(s.transmitters.ExpectedTransmitters(), s.doppler, s.now(), centerHz, spanHz)
}

// MarkersInBand сдвигает номиналы на Доплер КА по той же формуле, что канал, и оставляет
// попавшие в окно центр ± полоса/2. КА без известной орбиты пропускается: где он звучит, неизвестно.
func MarkersInBand(txs []ExpectedTransmitter, doppler DopplerSource, now time.Time,
	centerHz uint64, spanHz uint32) []TxMarker {
	if doppler == nil || len(txs) == 0 {
		return nil
	}
	lo := float64(centerHz) - float64(spanHz)/2
	hi := float64(centerHz) + float64(spanHz)/2
	var out []TxMarker
	rates := make(map[int]*float64, len(txs))
	for _, tx := range txs {
		rr, seen := rates[tx.NoradID]
		if !seen {
			if v, ok := doppler.RangeRateKmps(tx.NoradID, now); ok {
				rr = &v
			}
			rates[tx.NoradID] = rr
		}
		if rr == nil {
			continue
		}
		shift := tracker.DopplerShiftHz(float64(tx.FreqHz), *rr)
		f := float64(tx.FreqHz) + shift
		if f < lo || f > hi {
			continue
		}
		out = append(out, TxMarker{
			NoradID:   tx.NoradID,
			UUID:      tx.UUID,
			FreqHz:    int64(math.Round(f)),
			DopplerHz: math.Round(shift*10) / 10,
		})
	}
	return out
}
