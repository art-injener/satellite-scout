package handlers

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/registry"
	"github.com/art-injener/satellite-scout/internal/rfprofile"
)

// SatRadioHandler — GET /api/sat-radio (снимок радиопрофилей КА).
type SatRadioHandler struct {
	reg *registry.Registry
}

// NewSatRadioHandler создаёт обработчик снимка радиопрофилей.
func NewSatRadioHandler(reg *registry.Registry) *SatRadioHandler {
	return &SatRadioHandler{reg: reg}
}

// SatRadioResponse — ответ GET /api/sat-radio.
type SatRadioResponse struct {
	Satellites []SatRadioSat `json:"satellites"`
}

// SatRadioSat — сводка радиопрофиля одного КА для UI.
type SatRadioSat struct {
	NoradID     int          `json:"norad_id"`
	Heard       bool         `json:"heard"`
	FramesTotal int          `json:"frames_total"`
	BestDemod   string       `json:"best_demod,omitempty"`
	UpdatedMs   int64        `json:"updated_ms,omitempty"`
	Tx          []SatRadioTx `json:"tx"`
}

// SatRadioTx — передатчик: действующий режим и заявлено при расхождении.
type SatRadioTx struct {
	Key                 string  `json:"key"`
	UUID                string  `json:"uuid,omitempty"`
	FreqHz              int64   `json:"freq_hz"`
	EffectiveSource     string  `json:"effective_source,omitempty"`
	EffectiveDemod      string  `json:"effective_demod,omitempty"`
	EffectiveModulation string  `json:"effective_modulation,omitempty"`
	EffectiveBaud       float64 `json:"effective_baud,omitempty"`
	DeclaredMode        string  `json:"declared_mode,omitempty"`
	DeclaredBaud        float64 `json:"declared_baud,omitempty"`
	DeclaredDiffers     bool    `json:"declared_differs,omitempty"`
	VisitsWithEnergy    int     `json:"visits_with_energy,omitempty"`
	FramesTotal         int     `json:"frames_total,omitempty"`
	LastHeardMs         int64   `json:"last_heard_ms,omitempty"`
}

// Get обрабатывает GET /api/sat-radio[?norad=].
func (h *SatRadioHandler) Get(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.reg == nil {
		writeJSON(w, http.StatusOK, SatRadioResponse{Satellites: []SatRadioSat{}})
		return
	}
	noradStr := r.URL.Query().Get("norad")
	if noradStr != "" {
		norad, err := strconv.Atoi(noradStr)
		if err != nil || norad <= 0 {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid norad"})
			return
		}
		p, ok := h.reg.RFProfile(norad)
		if !ok {
			writeJSON(w, http.StatusOK, SatRadioResponse{Satellites: []SatRadioSat{}})
			return
		}
		writeJSON(w, http.StatusOK, SatRadioResponse{
			Satellites: []SatRadioSat{BuildSatRadioSat(h.reg, p)},
		})
		return
	}
	profiles := h.reg.RFProfiles()
	out := make([]SatRadioSat, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, BuildSatRadioSat(h.reg, p))
	}
	writeJSON(w, http.StatusOK, SatRadioResponse{Satellites: out})
}

// BuildSatRadioSat собирает UI-снимок из профиля и заявленных TX реестра.
func BuildSatRadioSat(reg *registry.Registry, p rfprofile.Profile) SatRadioSat {
	sat := SatRadioSat{
		NoradID:   p.NoradID,
		UpdatedMs: p.UpdatedMs,
		Tx:        make([]SatRadioTx, 0, len(p.Tx)),
	}
	var declared []freqplan.Tx
	if reg != nil {
		declared = reg.Transmitters(p.NoradID)
	}
	for _, tx := range p.Tx {
		row := satRadioTx(tx, declared)
		sat.Tx = append(sat.Tx, row)
		sat.FramesTotal += row.FramesTotal
		if row.VisitsWithEnergy > 0 || row.LastHeardMs > 0 {
			sat.Heard = true
		}
		if p.BestKey != "" && tx.Key == p.BestKey && row.EffectiveDemod != "" {
			sat.BestDemod = row.EffectiveDemod
		}
	}
	if sat.BestDemod == "" {
		for _, row := range sat.Tx {
			if row.EffectiveDemod != "" {
				sat.BestDemod = row.EffectiveDemod
				break
			}
		}
	}
	return sat
}

func satRadioTx(tx rfprofile.Transmitter, declared []freqplan.Tx) SatRadioTx {
	row := SatRadioTx{
		Key:              tx.Key,
		UUID:             tx.UUID,
		FreqHz:           tx.FreqHz,
		VisitsWithEnergy: tx.Stats.VisitsWithEnergy,
		FramesTotal:      tx.Accepted.FramesTotal,
		LastHeardMs:      tx.Stats.LastHeardMs,
	}
	eff := rfprofile.Effective(tx)
	row.EffectiveSource = eff.Source
	row.EffectiveDemod = eff.Demod
	row.EffectiveModulation = eff.Modulation
	row.EffectiveBaud = eff.Baud
	if mode, baud, ok := matchDeclared(declared, tx); ok {
		row.DeclaredMode = mode
		row.DeclaredBaud = baud
		if eff.Source != "" {
			row.DeclaredDiffers = declaredDiffers(eff, mode, baud)
		}
	}
	if row.EffectiveSource == "" && row.DeclaredMode != "" {
		row.EffectiveSource = "declared"
		row.EffectiveModulation = row.DeclaredMode
		row.EffectiveBaud = row.DeclaredBaud
	}
	return row
}

func matchDeclared(declared []freqplan.Tx, tx rfprofile.Transmitter) (mode string, baud float64, ok bool) {
	for _, d := range declared {
		if tx.UUID != "" && d.UUID == tx.UUID {
			return d.Mode, d.Baud, true
		}
	}
	for _, d := range declared {
		if math.Abs(float64(d.FreqHz-tx.FreqHz)) <= float64(freqplan.NearHz) {
			return d.Mode, d.Baud, true
		}
	}
	return "", 0, false
}

func declaredDiffers(eff rfprofile.EffectiveMode, mode string, baud float64) bool {
	if eff.Modulation != "" && mode != "" && !modeMatches(eff.Modulation, mode) {
		return true
	}
	if eff.Baud > 0 && baud > 0 && math.Abs(eff.Baud-baud) > 1 {
		return true
	}
	return false
}

func modeMatches(observed, declared string) bool {
	if observed == "" || declared == "" {
		return true
	}
	if strings.EqualFold(observed, declared) {
		return true
	}
	if strings.EqualFold(observed, "2FSK") &&
		(strings.Contains(strings.ToUpper(declared), "FSK") ||
			strings.Contains(strings.ToUpper(declared), "G3RUH")) {
		return true
	}
	return strings.Contains(strings.ToUpper(declared), strings.ToUpper(observed))
}
