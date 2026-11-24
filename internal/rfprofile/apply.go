package rfprofile

import "github.com/art-injener/satellite-scout/internal/freqplan"

// AcceptedSilenceMs — после столь долгого отсутствия кадров слой «принято» сбрасывается.
const AcceptedSilenceMs = 7 * 24 * 60 * 60 * 1000 // 7 суток

// ApplyDwell обновляет профиль по итогам одной стоянки для данного NORAD.
// Визиты других NORAD игнорируются. sights обогащают слой «обнаружено».
func ApplyDwell(p Profile, visits []freqplan.Visit, sights []freqplan.Sighting) Profile {
	for _, v := range visits {
		if v.NoradID == 0 || (p.NoradID != 0 && v.NoradID != p.NoradID) {
			continue
		}
		if p.NoradID == 0 {
			p.NoradID = v.NoradID
		}
		tx := ensureTx(&p, v.UUID, v.FreqHz)
		tx.Stats.VisitsTotal++
		if v.WindowEndMs > p.UpdatedMs {
			p.UpdatedMs = v.WindowEndMs
		}
		if v.Energy {
			tx.Stats.VisitsWithEnergy++
			if v.WindowEndMs > tx.Stats.LastHeardMs {
				tx.Stats.LastHeardMs = v.WindowEndMs
			}
			if v.SNRDB > tx.Stats.BestSNRDB {
				tx.Stats.BestSNRDB = v.SNRDB
			}
			if s, ok := freqplan.MatchSighting(sights, v.NoradID, v.FreqHz); ok {
				applyDetected(tx, s, v.WindowEndMs)
			}
		}
		clearAcceptedIfSilent(tx, v.WindowEndMs)
	}
	updateBestKey(&p)
	return p
}

// ApplyFrame фиксирует принятый кадр с верной CRC.
func ApplyFrame(p Profile, uuid string, freqHz int64, demodID string, tsMs int64) Profile {
	if p.NoradID == 0 || freqHz <= 0 || demodID == "" {
		return p
	}
	tx := ensureTx(&p, uuid, freqHz)
	tx.Accepted.DemodID = demodID
	tx.Accepted.FramesTotal++
	tx.Accepted.LastFrameMs = tsMs
	tx.Stats.VisitsWithFrames++
	if tsMs > tx.Stats.LastHeardMs {
		tx.Stats.LastHeardMs = tsMs
	}
	if tsMs > p.UpdatedMs {
		p.UpdatedMs = tsMs
	}
	updateBestKey(&p)
	return p
}

func ensureTx(p *Profile, uuid string, freqHz int64) *Transmitter {
	key := Key(uuid, freqHz)
	for i := range p.Tx {
		if p.Tx[i].Key == key {
			return &p.Tx[i]
		}
		// Совпадение по частоте без UUID: ключ мог быть только номиналом.
		if uuid == "" && nearHz(p.Tx[i].FreqHz, freqHz) {
			return &p.Tx[i]
		}
		if uuid != "" && p.Tx[i].UUID == uuid {
			return &p.Tx[i]
		}
	}
	p.Tx = append(p.Tx, Transmitter{
		Key:    key,
		UUID:   uuid,
		FreqHz: QuantizeHz(freqHz),
	})
	return &p.Tx[len(p.Tx)-1]
}

func applyDetected(tx *Transmitter, s freqplan.Sighting, atMs int64) {
	if s.Modulation == "" && s.Baud == 0 {
		return
	}
	same := tx.Detected.Modulation == s.Modulation && tx.Detected.Baud == s.Baud
	if same && tx.Detected.Confirmations > 0 {
		tx.Detected.Confirmations++
	} else {
		tx.Detected.Confirmations = 1
		tx.Detected.Modulation = s.Modulation
		tx.Detected.Baud = s.Baud
	}
	if s.BandwidthHz > 0 {
		tx.Detected.BandwidthHz = s.BandwidthHz
	}
	if s.BeaconPeriodS > 0 {
		tx.Detected.BeaconPeriodS = s.BeaconPeriodS
	}
	if s.Confidence > 0 {
		tx.Detected.Confidence = s.Confidence
	}
	if s.Demod != "" {
		tx.Detected.Demod = s.Demod
	}
	if len(s.DemodAlt) > 0 {
		tx.Detected.DemodAlt = append([]string(nil), s.DemodAlt...)
	}
	tx.Detected.OffsetHz = float64(s.CenterHz - tx.FreqHz)
	if atMs > 0 {
		tx.Detected.LastSeenMs = atMs
	} else if s.LastSeenMs > 0 {
		tx.Detected.LastSeenMs = s.LastSeenMs
	}
}

func clearAcceptedIfSilent(tx *Transmitter, nowMs int64) {
	if tx.Accepted.DemodID == "" || tx.Accepted.LastFrameMs == 0 || nowMs == 0 {
		return
	}
	if nowMs-tx.Accepted.LastFrameMs > AcceptedSilenceMs {
		tx.Accepted = Accepted{}
	}
}

func updateBestKey(p *Profile) {
	best := ""
	bestEnergy := -1
	var bestSNR float32
	for _, tx := range p.Tx {
		if tx.Stats.VisitsWithEnergy > bestEnergy ||
			(tx.Stats.VisitsWithEnergy == bestEnergy && tx.Stats.BestSNRDB > bestSNR) {
			best = tx.Key
			bestEnergy = tx.Stats.VisitsWithEnergy
			bestSNR = tx.Stats.BestSNRDB
		}
	}
	p.BestKey = best
}

func nearHz(a, b int64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= int64(freqplan.NearHz)
}
