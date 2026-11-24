package main

import (
	"github.com/art-injener/satellite-scout/internal/bandscan"
	"github.com/art-injener/satellite-scout/internal/demod"
)

// airHearAdapter — demod.AirHear → bandscan.Service.RecordRx.
type airHearAdapter struct {
	band *bandscan.Service
}

func (a airHearAdapter) RecordRx(pathID string, freqHz uint64, note demod.AirNote) {
	if a.band == nil {
		return
	}
	hn := bandscan.HearNote{
		Kind: note.Kind, Summary: note.Summary, Decoder: note.Decoder,
		Src: note.Src, Dest: note.Dest, Info: note.Info, InfoHex: note.InfoHex,
		TimestampMs: note.TimestampMs, NoradID: note.NoradID, SatName: note.SatName,
		CountFrame: note.CountFrame,
	}
	if len(note.Params) > 0 {
		hn.Params = make([]bandscan.HearParam, len(note.Params))
		for i, p := range note.Params {
			hn.Params[i] = bandscan.HearParam{
				ID: p.ID, Name: p.Name, Value: p.Value, Unit: p.Unit, Key: p.Key,
			}
		}
	}
	a.band.RecordRx(pathID, freqHz, hn)
}
