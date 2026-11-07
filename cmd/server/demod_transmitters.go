package main

import (
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
