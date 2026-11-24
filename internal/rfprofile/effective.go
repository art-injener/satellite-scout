package rfprofile

import "github.com/art-injener/satellite-scout/internal/freqplan"

// Источник действующего режима.
const (
	SourceAccepted = "accepted"
	SourceDetected = "detected"
)

// EffectiveMode — действующий режим передатчика (ADR-012 §2.2).
type EffectiveMode struct {
	Source        string // accepted | detected | пусто (остаётся заявлено у потребителя)
	Demod         string
	DemodAlt      []string
	Modulation    string
	Baud          float64
	BeaconPeriodS float64
}

// Effective возвращает действующий режим: принято → обнаружено (≥3) → пусто.
func Effective(tx Transmitter) EffectiveMode {
	if tx.Accepted.DemodID != "" {
		return EffectiveMode{
			Source: SourceAccepted,
			Demod:  tx.Accepted.DemodID,
		}
	}
	if tx.Detected.Confirmations >= MinDetectedConfirmations {
		return EffectiveMode{
			Source:        SourceDetected,
			Demod:         tx.Detected.Demod,
			DemodAlt:      append([]string(nil), tx.Detected.DemodAlt...),
			Modulation:    tx.Detected.Modulation,
			Baud:          tx.Detected.Baud,
			BeaconPeriodS: tx.Detected.BeaconPeriodS,
		}
	}
	return EffectiveMode{}
}

// FindTx ищет передатчик по UUID или частоте (±NearHz).
func FindTx(p Profile, uuid string, freqHz int64) (Transmitter, bool) {
	key := Key(uuid, freqHz)
	for _, tx := range p.Tx {
		if tx.Key == key {
			return tx, true
		}
		if uuid != "" && tx.UUID == uuid {
			return tx, true
		}
		if nearFreq(tx.FreqHz, freqHz) {
			return tx, true
		}
	}
	return Transmitter{}, false
}

// LastHeardAt — время последней энергии/кадра у TX на частоте; 0 — не слышали.
func LastHeardAt(p Profile, freqHz int64) int64 {
	tx, ok := FindTx(p, "", freqHz)
	if !ok {
		return 0
	}
	return tx.Stats.LastHeardMs
}

// BeaconPeriodS — период маяка из обнаруженного слоя, если есть.
func BeaconPeriodS(p Profile, freqHz int64) float64 {
	tx, ok := FindTx(p, "", freqHz)
	if !ok {
		return 0
	}
	if tx.Detected.BeaconPeriodS > 0 {
		return tx.Detected.BeaconPeriodS
	}
	return 0
}

func nearFreq(a, b int64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= int64(freqplan.NearHz)
}
