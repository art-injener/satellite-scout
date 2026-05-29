package main

import (
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/services"
)

// satnogsTransmitterAdapter — адаптер satnogs.Service → services.TransmitterProvider.
// Преобразует satnogs.TransmitterSummary в services.TransmitterInfo,
// чтобы пакет services не зависел от пакета satnogs.
type satnogsTransmitterAdapter struct {
	svc *satnogs.Service
}

// newSatnogsTransmitterAdapter оборачивает satnogs.Service в интерфейс services.TransmitterProvider.
func newSatnogsTransmitterAdapter(svc *satnogs.Service) *satnogsTransmitterAdapter {
	return &satnogsTransmitterAdapter{svc: svc}
}

// GetPrimaryTransmitter — реализация интерфейса services.TransmitterProvider.
func (a *satnogsTransmitterAdapter) GetPrimaryTransmitter(noradID int) *services.TransmitterInfo {
	if a == nil || a.svc == nil {
		return nil
	}
	tx := a.svc.GetPrimaryTransmitter(noradID)
	if tx == nil {
		return nil
	}
	return &services.TransmitterInfo{
		FreqMHz:    tx.FreqMHz,
		Modulation: tx.Modulation,
	}
}

// RequestFetch — реализация интерфейса services.TransmitterProvider.
func (a *satnogsTransmitterAdapter) RequestFetch(noradIDs []int) {
	if a == nil || a.svc == nil {
		return
	}
	a.svc.RequestFetch(noradIDs)
}
