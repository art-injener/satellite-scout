package main

import (
	"log/slog"
	"sync"
	"time"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sweep"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// holdAimByPath — наведение OffsetHz на пик вспышки: один вход + одна live-поправка на стоянку.
type holdAimByPath struct {
	mu   sync.Mutex
	byID map[string]freqplan.AimState
}

// lockHoldAim — оператор сменил сдвиг канала: до конца стоянки автонаведение молчит.
func (a *app) lockHoldAim(pathID string) {
	if a.holdAim == nil || pathID == "" {
		return
	}
	a.holdAim.mu.Lock()
	defer a.holdAim.mu.Unlock()
	st := a.holdAim.byID[pathID]
	st.OperatorLocked = true
	a.holdAim.byID[pathID] = st
}

// keepHoldAimOffset — hold уже наведён: POST offset=0 не должен сбрасывать пик.
func (a *app) keepHoldAimOffset(pathID string) bool {
	if a.holdAim == nil || pathID == "" {
		return false
	}
	a.holdAim.mu.Lock()
	defer a.holdAim.mu.Unlock()
	st := a.holdAim.byID[pathID]
	return st.Phase == freqplan.AimEntry || st.Phase == freqplan.AimLive
}

// maybeAimHoldChannel — при hold выставить OffsetHz на центр участка bandscan (не каждый БПФ).
func (a *app) maybeAimHoldChannel(pathID string, w sweep.Window, startMs, nowMs int64, live []freqplan.Sighting) {
	if a.holdAim == nil || a.receiverHub == nil || pathID == "" || w.CenterHz == 0 {
		return
	}
	cs, err := a.receiverHub.Channel(pathID)
	if err != nil {
		return
	}
	remembered := a.rememberedAimSights(pathID, nowMs)

	dirty := false
	// Окно «+эфир»: чужой NORAD с прошлого TX уводит канал Доплером мимо вспышки.
	if w.Air && cs.DopplerNorad != 0 {
		cs.DopplerNorad = 0
		dirty = true
	}
	dopplerHz := 0.0
	if !w.Air {
		dopplerHz = a.channelDopplerHz(cs, w.CenterHz)
	}

	a.holdAim.mu.Lock()
	st := a.holdAim.byID[pathID]
	newDwell := st.StartMs != startMs || st.CenterHz != w.CenterHz
	if newDwell {
		st = freqplan.ResetAimState(startMs, w.CenterHz)
	}
	dec, next := freqplan.DecideHoldAim(st, w.CenterHz, remembered, live, nowMs, dopplerHz)
	a.holdAim.byID[pathID] = next
	a.holdAim.mu.Unlock()

	if newDwell && cs.OffsetHz != 0 && !dec.Apply {
		cs.OffsetHz = 0
		dirty = true
	}
	if dec.Apply {
		cs.OffsetHz = dec.OffsetHz
		dirty = true
	}
	if !dirty {
		return
	}
	if err := a.receiverHub.SetChannel(pathID, cs); err != nil {
		slog.Warn("hold aim: set channel",
			slog.String("radio_path", pathID),
			slog.Int64("offset_hz", cs.OffsetHz),
			slog.Int("doppler_norad", cs.DopplerNorad),
			slogKeyError, err)
		return
	}
	if dec.Apply {
		slog.Info("hold aim: channel centered on flash",
			slog.String("radio_path", pathID),
			slog.Uint64("lo_hz", w.CenterHz),
			slog.Int64("sight_hz", dec.SightHz),
			slog.Int64("offset_hz", dec.OffsetHz),
			slog.Float64("doppler_hz", dopplerHz))
	} else if w.Air {
		slog.Info("hold aim: cleared doppler on air hop",
			slog.String("radio_path", pathID),
			slog.Uint64("lo_hz", w.CenterHz))
	}
}

// rememberedAimSights — память bandscan для входного наведения; LastSeen = now, чтобы GoneMs не отсек час.
func (a *app) rememberedAimSights(pathID string, nowMs int64) []freqplan.Sighting {
	if a.bandscan == nil {
		return nil
	}
	var out []freqplan.Sighting
	for _, s := range a.bandscan.RememberedSignals(pathID) {
		if s.SNRDB < freqplan.AimMinSNRDB {
			continue
		}
		out = append(out, freqplan.Sighting{
			CenterHz:    s.CenterHz,
			NoradID:     s.NoradID,
			Origin:      s.Origin,
			Emission:    s.Emission,
			SNRDB:       s.SNRDB,
			LastSeenMs:  nowMs,
			BandwidthHz: s.BandwidthHz,
		})
	}
	return out
}

// channelDopplerHz — та же поправка, что цепочка демода прибавит к OffsetHz.
func (a *app) channelDopplerHz(cs sdr.ChannelSettings, loHz uint64) float64 {
	if !cs.DopplerAuto {
		return float64(cs.DopplerManualHz)
	}
	if cs.DopplerNorad == 0 || a.tracking == nil {
		return 0
	}
	rr, ok := a.tracking.RangeRateKmps(cs.DopplerNorad, time.Now().UTC())
	if !ok {
		return 0
	}
	return tracker.DopplerShiftHz(float64(loHz), rr)
}
