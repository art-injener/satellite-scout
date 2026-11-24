package demod

import (
	"math"
	"strings"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// ProfileMeta — метаданные профиля демодулятора (ADR-009 §4.1).
// Реализация цепочки живёт в map profiles; сюда только описание для каталога и кандидатов.
type ProfileMeta struct {
	ID                 string   `json:"id"`
	Label              string   `json:"label"`
	Family             string   `json:"family"`
	Baud               float64  `json:"baud"` // символьная скорость; 0 — без фиксированной
	DefaultBandwidthHz uint32   `json:"default_bandwidth_hz"`
	SatNOGSModes       []string `json:"satnogs_modes,omitempty"`
	BaudFlexible       bool     `json:"baud_flexible"` // скорость из канала / SatNOGS не отсекает
}

// metaByID — полный реестр id из sdr.Demods. Порядок списка — порядок Demods.
// Профиль «auto» — режим подбора, не модуляция: в реестре метаданных модуляций его нет.
var metaByID = map[string]ProfileMeta{
	"fsk9600_g3ruh": {
		ID: "fsk9600_g3ruh", Label: "FSK 9600 G3RUH", Family: "FSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"FSK", "GFSK", "2FSK"},
	},
	"fsk9600_geoscan": {
		ID: "fsk9600_geoscan", Label: "FSK 9600 GEOSCAN", Family: "FSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"FSK", "GFSK", "2FSK"},
	},
	"fsk9600_ax100_rs": {
		ID: "fsk9600_ax100_rs", Label: "FSK 9600 AX.100 RS", Family: "FSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"FSK", "GFSK", "2FSK"},
	},
	"fsk9600": {
		ID: "fsk9600", Label: "FSK 9600 линия", Family: "FSK",
		Baud: 9600, DefaultBandwidthHz: 15_000,
		BaudFlexible: true,
	},
	"fsk4800": {
		ID: "fsk4800", Label: "FSK 4800", Family: "FSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"FSK", "GFSK", "2FSK"},
	},
	"fsk4800_ax100": {
		ID: "fsk4800_ax100", Label: "FSK 4800 AX.100", Family: "FSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"FSK", "GFSK", "2FSK"},
	},
	"fsk1200": {
		ID: "fsk1200", Label: "FSK 1200", Family: "FSK",
		Baud: 1200, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"FSK", "GFSK", "2FSK"},
	},
	"gfsk9600": {
		ID: "gfsk9600", Label: "GFSK 9600", Family: "GFSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"GFSK"},
	},
	"gfsk4800": {
		ID: "gfsk4800", Label: "GFSK 4800", Family: "GFSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GFSK"},
	},
	"gfsk1200": {
		ID: "gfsk1200", Label: "GFSK 1200", Family: "GFSK",
		Baud: 1200, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GFSK"},
	},
	"gmsk9600": {
		ID: "gmsk9600", Label: "GMSK 9600", Family: "GMSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk9600_ax100": {
		ID: "gmsk9600_ax100", Label: "GMSK 9600 AX.100", Family: "GMSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk9600_ax100_rs": {
		ID: "gmsk9600_ax100_rs", Label: "GMSK 9600 AX.100 RS", Family: "GMSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk4800": {
		ID: "gmsk4800", Label: "GMSK 4800", Family: "GMSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk4800_ax100": {
		ID: "gmsk4800_ax100", Label: "GMSK 4800 AX.100", Family: "GMSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk4800_usp": {
		ID: "gmsk4800_usp", Label: "GMSK 4800 USP", Family: "GMSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk2400": {
		ID: "gmsk2400", Label: "GMSK 2400", Family: "GMSK",
		Baud: 2400, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk2400_usp": {
		ID: "gmsk2400_usp", Label: "GMSK 2400 USP", Family: "GMSK",
		Baud: 2400, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GMSK"},
	},
	"gmsk9600_usp": {
		ID: "gmsk9600_usp", Label: "GMSK 9600 USP", Family: "GMSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"GMSK"},
	},
	"msk9600": {
		ID: "msk9600", Label: "MSK 9600", Family: "MSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"MSK"},
	},
	"3fsk4800": {
		ID: "3fsk4800", Label: "3FSK 4800", Family: "3FSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"3FSK", "FSK"},
	},
	"4fsk4800": {
		ID: "4fsk4800", Label: "4FSK 4800", Family: "4FSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"4FSK", "DMR"},
	},
	"gmfsk4800": {
		ID: "gmfsk4800", Label: "GMFSK 4800", Family: "GMFSK",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GMFSK", "4FSK"},
	},
	"afsk1200": {
		ID: "afsk1200", Label: "AFSK 1200", Family: "AFSK",
		Baud: 1200, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"AFSK"},
	},
	"bpsk1200": {
		ID: "bpsk1200", Label: "BPSK 1200", Family: "BPSK",
		Baud: 1200, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"BPSK"},
	},
	"bpsk9600": {
		ID: "bpsk9600", Label: "BPSK 9600", Family: "BPSK",
		Baud: 9600, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"BPSK"},
	},
	"qpsk_ccsds": {
		ID: "qpsk_ccsds", Label: "QPSK CCSDS", Family: "QPSK",
		Baud: 0, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"QPSK"}, BaudFlexible: true,
	},
	"pi4_dqpsk": {
		ID: "pi4_dqpsk", Label: "π/4-DQPSK", Family: "DQPSK",
		Baud: 0, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"DQPSK", "PI4DQPSK", "π/4-DQPSK"}, BaudFlexible: true,
	},
	"apsk16": {
		ID: "apsk16", Label: "16-APSK", Family: "APSK",
		Baud: 0, DefaultBandwidthHz: sdr.MaxChannelBandwidthHz,
		SatNOGSModes: []string{"APSK", "16APSK"}, BaudFlexible: true,
	},
	"cw": {
		ID: "cw", Label: "CW", Family: "CW",
		Baud: 0, DefaultBandwidthHz: sdr.DemodMaxBandwidthHz["cw"],
		SatNOGSModes: []string{"CW"},
	},
	"am": {
		ID: "am", Label: "AM", Family: "AM",
		Baud: 0, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"AM"},
	},
	"fm": {
		ID: "fm", Label: "FM", Family: "FM",
		Baud: 0, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"FM", "NFM"},
	},
	"wfm": {
		ID: "wfm", Label: "FM эфир", Family: "WFM",
		Baud: 0, DefaultBandwidthHz: sdr.WFMBandwidthHz,
		SatNOGSModes: []string{"WFM"},
	},
	"usb": {
		ID: "usb", Label: "USB", Family: "USB",
		Baud: 0, DefaultBandwidthHz: sdr.DemodMaxBandwidthHz["usb"],
		SatNOGSModes: []string{"USB", "SSB"},
	},
	"lsb": {
		ID: "lsb", Label: "LSB", Family: "LSB",
		Baud: 0, DefaultBandwidthHz: sdr.DemodMaxBandwidthHz["lsb"],
		SatNOGSModes: []string{"LSB", "SSB"},
	},
	"ook": {
		ID: "ook", Label: "OOK", Family: "OOK",
		Baud: 0, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"OOK"}, BaudFlexible: true,
	},
	"rs41": {
		ID: "rs41", Label: "RS41", Family: "RS41",
		Baud: 4800, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"GFSK", "RS41"},
	},
	"m10": {
		ID: "m10", Label: "M10", Family: "M10",
		Baud: 9615, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"FSK", "M10"},
	},
	"sstv": {
		ID: "sstv", Label: "SSTV", Family: "SSTV",
		Baud: 0, DefaultBandwidthHz: 15_000,
		SatNOGSModes: []string{"SSTV"},
	},
}

// Meta возвращает метаданные профиля по id.
func Meta(id string) (ProfileMeta, bool) {
	m, ok := metaByID[id]
	return m, ok
}

// ListMeta — метаданные профилей модуляции в порядке sdr.Demods (без режима «Авто»).
func ListMeta() []ProfileMeta {
	out := make([]ProfileMeta, 0, len(sdr.Demods))
	for _, id := range sdr.Demods {
		if id == DemodAuto {
			continue
		}
		if m, ok := metaByID[id]; ok {
			out = append(out, m)
		}
	}
	return out
}

// ListProfilesAPI — профили для селектора UI: «Авто», затем ListMeta.
func ListProfilesAPI() []ProfileMeta {
	out := make([]ProfileMeta, 0, 1+len(sdr.Demods))
	out = append(out, ProfileMeta{ID: DemodAuto, Label: "Авто", Family: "AUTO"})
	return append(out, ListMeta()...)
}

// Candidates — id профилей, совместимых с режимом и скоростью SatNOGS.
// Без автовыбора: только список. Порядок как в sdr.Demods.
func Candidates(mode string, baud float64) []string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	want := normalizeMode(mode)
	baudKey := roundBaud(baud)
	var out []string
	for _, id := range sdr.Demods {
		m, ok := metaByID[id]
		if !ok {
			continue
		}
		if !modeMatches(m, want) {
			continue
		}
		if !baudMatches(m, baudKey) {
			continue
		}
		out = append(out, id)
	}
	return out
}

func normalizeMode(mode string) string {
	return strings.ToUpper(strings.TrimSpace(mode))
}

func roundBaud(baud float64) float64 {
	if baud <= 0 || math.IsNaN(baud) || math.IsInf(baud, 0) {
		return 0
	}
	return math.Round(baud)
}

func modeMatches(m ProfileMeta, want string) bool {
	for _, s := range m.SatNOGSModes {
		if normalizeMode(s) == want {
			return true
		}
	}
	return false
}

func baudMatches(m ProfileMeta, baudKey float64) bool {
	if m.BaudFlexible {
		return true
	}
	if m.Baud > 0 {
		// Цифровой профиль с фиксированной скоростью: без baud из SatNOGS не кандидат.
		if baudKey <= 0 {
			return false
		}
		return roundBaud(m.Baud) == baudKey
	}
	// Голос / CW / SSTV: только без скорости.
	return baudKey == 0
}
