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
	count := 0
	all := a.svc.GetAllTransmitters(noradID)
	for i := range all {
		t := &all[i]
		if t.IsActive() && t.HasDownlink() {
			count++
		}
	}
	if count == 0 {
		count = 1 // primary есть — минимум один TX
	}
	return &services.TransmitterInfo{
		UUID:       tx.UUID,
		FreqMHz:    tx.FreqMHz,
		Modulation: tx.Modulation,
		Count:      count,
	}
}

// RequestFetch — реализация интерфейса services.TransmitterProvider.
func (a *satnogsTransmitterAdapter) RequestFetch(noradIDs []int) {
	if a == nil || a.svc == nil {
		return
	}
	a.svc.RequestFetch(noradIDs)
}

// ListActiveTransmitters — реализация интерфейса services.TxCatalog.
// Отдаёт UUID только тех передатчиков, у которых есть downlink и которые
// в активном статусе (alive + status=active).
// Если полный список пуст/отфильтрован, но primary уже есть — отдаём его UUID,
// чтобы mock tx_cycle и auto-link не оставались без данных имитации.
func (a *satnogsTransmitterAdapter) ListActiveTransmitters(noradID int) []services.TransmitterRef {
	if a == nil || a.svc == nil {
		return nil
	}
	all := a.svc.GetAllTransmitters(noradID)
	out := make([]services.TransmitterRef, 0, len(all))
	for i := range all {
		t := &all[i]
		if !t.IsActive() || !t.HasDownlink() {
			continue
		}
		if t.UUID == "" {
			continue
		}
		out = append(out, services.TransmitterRef{UUID: t.UUID})
	}
	if len(out) > 0 {
		return out
	}
	if tx := a.svc.GetPrimaryTransmitter(noradID); tx != nil && tx.UUID != "" {
		return []services.TransmitterRef{{UUID: tx.UUID}}
	}
	return nil
}
