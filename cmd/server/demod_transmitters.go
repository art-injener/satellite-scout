package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/art-injener/satellite-scout/internal/bandscan"
	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/demod"
	"github.com/art-injener/satellite-scout/internal/services"
	"github.com/art-injener/satellite-scout/internal/sigmf"
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
		out[i] = demod.ExpectedTransmitter{
			NoradID: tx.NoradID, UUID: tx.UUID, FreqHz: tx.FreqHz,
			Mode: tx.Mode, Baud: tx.Baud,
		}
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
		out[i] = bandscan.Expected{
			NoradID: m.NoradID, UUID: m.UUID, FreqHz: float64(m.FreqHz),
			SatName: b.tracking.SatName(m.NoradID),
		}
	}
	return out
}

// pathSatHints собирает подсказки КА для трактов replay: SigMF scout:* важнее norad_id в конфиге.
func pathSatHints(paths []config.RadioPath) demod.PathSatMap {
	out := make(demod.PathSatMap)
	for _, rp := range paths {
		rc := rp.Receiver
		if rc.Imitator == nil {
			continue
		}
		pathID := fmt.Sprintf("radio_path_%d", rp.ID)
		im := rc.Imitator
		if im.Replay != "" {
			if m, err := sigmf.LoadMeta(im.Replay); err == nil && m.HasScoutSat() {
				out[pathID] = demod.PathSatHint{
					NoradID: m.Global.NoradID,
					SatName: m.Global.SatName,
					Source:  demod.SatSourceSigMF,
				}
				continue
			} else if err != nil {
				slog.Warn("demod: sigmf meta for path sat hint",
					slog.String("path", pathID), slog.String("meta", im.Replay), slog.Any("error", err))
			}
		}
		if im.NoradID > 0 {
			out[pathID] = demod.PathSatHint{
				NoradID: im.NoradID,
				SatName: im.SatName,
				Source:  demod.SatSourceReplay,
			}
		}
	}
	return out
}
