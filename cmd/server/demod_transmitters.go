package main

import (
	"time"

	"github.com/art-injener/satellite-scout/internal/bandscan"
	"github.com/art-injener/satellite-scout/internal/demod"
	"github.com/art-injener/satellite-scout/internal/services"
)

// demodTransmitters — адаптер сервиса сопровождения в demod.TransmitterSource:
// маркеры на спектре ставятся по передатчикам КА группы Плана сеансов.
type demodTransmitters struct {
	tracking *services.SatelliteTrackingService
}

// ExpectedTransmitters — реализация интерфейса demod.TransmitterSource.
func (d demodTransmitters) ExpectedTransmitters() []demod.ExpectedTransmitter {
	list := d.tracking.GroupTransmitters()
	if len(list) == 0 {
		return nil
	}
	out := make([]demod.ExpectedTransmitter, len(list))
	for i, tx := range list {
		out[i] = demod.ExpectedTransmitter{NoradID: tx.NoradID, UUID: tx.UUID, FreqHz: tx.FreqHz}
	}
	return out
}

// bandExpected — адаптер в bandscan.ExpectedSource: те же передатчики и Доплер, что у рисок на спектре.
type bandExpected struct {
	tracking *services.SatelliteTrackingService
}

// ExpectedInBand — реализация интерфейса bandscan.ExpectedSource.
func (b bandExpected) ExpectedInBand(centerHz uint64, spanHz uint32) []bandscan.Expected {
	txs := demodTransmitters(b).ExpectedTransmitters()
	markers := demod.MarkersInBand(txs, b.tracking, time.Now(), centerHz, spanHz)
	out := make([]bandscan.Expected, len(markers))
	for i, m := range markers {
		out[i] = bandscan.Expected{NoradID: m.NoradID, UUID: m.UUID, FreqHz: float64(m.FreqHz)}
	}
	return out
}
