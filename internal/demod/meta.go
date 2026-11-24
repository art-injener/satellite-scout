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

// Candidates возвращает id профилей, совместимых с режимом и скоростью из реестра.
// Поддерживаются составные строки SatNOGS/sonik («GMSK USP», «FSK AX.100 Mode 5»,
// «FSK AX.25 G3RUH») и строки AMSAT со встроенными скоростями («2k4/4k8/9k6*GMSK_USP SSTV»).
// Профили с совпадением протокольного хинта идут первыми, далее порядок как в sdr.Demods.
func Candidates(mode string, baud float64) []string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return nil
	}
	parsed := parseModeSpec(mode, baud)
	if len(parsed.modes) == 0 && len(parsed.protos) == 0 {
		return nil
	}
	var preferred, rest []string
	for _, id := range sdr.Demods {
		m, ok := metaByID[id]
		if !ok {
			continue
		}
		if !specMatchesProfile(parsed, m) {
			continue
		}
		if parsed.matchesProto(id) {
			preferred = append(preferred, id)
		} else {
			rest = append(rest, id)
		}
	}
	return append(preferred, rest...)
}

// modeSpec — разобранная строка режима: токены модуляций, скоростей и протоколов.
type modeSpec struct {
	modes       map[string]bool
	bauds       map[float64]bool
	protos      map[string]bool
	argBaud     float64
	hasNonDigit bool // в строке помимо скоростей есть аналоговые токены (SSTV, CW, FM...)
}

func parseModeSpec(mode string, baud float64) modeSpec {
	sp := modeSpec{
		modes:   make(map[string]bool),
		bauds:   make(map[float64]bool),
		protos:  make(map[string]bool),
		argBaud: roundBaud(baud),
	}
	if sp.argBaud > 0 {
		sp.bauds[sp.argBaud] = true
	}
	rawTokens := strings.FieldsFunc(strings.ToUpper(mode), func(r rune) bool {
		switch r {
		case ' ', '\t', '/', '_', '*', ',', ';', '(', ')', '+':
			return true
		default:
			return false
		}
	})
	for _, tok := range rawTokens {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if b, tail, ok := parseBaudPrefix(tok); ok {
			sp.bauds[b] = true
			if tail == "" {
				continue
			}
			tok = tail
		}
		addModeToken(&sp, tok)
	}
	return sp
}

// parseBaudPrefix извлекает скорость вида «2K4», «4K8», «9K6», «1K2», «19K2», «9600»
// из начала токена и возвращает оставшийся хвост модуляции (например «9K6BPSK» даёт 9600 и «BPSK»).
func parseBaudPrefix(tok string) (float64, string, bool) {
	// Формат NkM: например 1K2, 2K4, 4K8, 9K6, 19K2, 38K4.
	if kIdx := strings.IndexByte(tok, 'K'); kIdx > 0 && kIdx <= 2 {
		if allDigits(tok[:kIdx]) {
			whole := parseIntSimple(tok[:kIdx])
			fracEnd := kIdx + 1
			for fracEnd < len(tok) && tok[fracEnd] >= '0' && tok[fracEnd] <= '9' {
				fracEnd++
			}
			fracStr := tok[kIdx+1 : fracEnd]
			var val float64
			switch len(fracStr) {
			case 0:
				val = float64(whole) * 1000
			case 1:
				val = float64(whole)*1000 + float64(parseIntSimple(fracStr))*100
			case 2:
				val = float64(whole)*1000 + float64(parseIntSimple(fracStr))*10
			default:
				return 0, "", false
			}
			if val >= 300 && val <= 100_000 {
				return math.Round(val), tok[fracEnd:], true
			}
		}
	}
	// Чистая десятичная скорость, например «9600», «4800», «2400», «1200».
	if allDigits(tok) && len(tok) >= 3 && len(tok) <= 6 {
		v := parseIntSimple(tok)
		if v >= 300 && v <= 100_000 {
			return float64(v), "", true
		}
	}
	return 0, "", false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseIntSimple(s string) int {
	n := 0
	for i := range len(s) {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

func addModeToken(sp *modeSpec, tok string) {
	compact := strings.NewReplacer(".", "", "-", "").Replace(tok)
	switch compact {
	case "USP":
		sp.protos["usp"] = true
		return
	case "AX100":
		sp.protos["ax100"] = true
		return
	case "G3RUH":
		sp.protos["g3ruh"] = true
		return
	case "GEOSCAN":
		sp.protos["geoscan"] = true
		return
	case "CCSDS":
		sp.protos["ccsds"] = true
		return
	case "FMN":
		sp.modes["FM"] = true
		sp.hasNonDigit = true
		return
	}
	// Токены аналоговых режимов: фиксируем, чтобы составные AMSAT-строки вроде «1k2 AFSK CW*»
	// могли подтягивать и цифровой, и аналоговый профиль, если аргумент baud == 0.
	switch tok {
	case "AM", "FM", "NFM", "WFM", "CW", "USB", "LSB", "SSB", "SSTV":
		sp.hasNonDigit = true
	}
	sp.modes[tok] = true
	if compact != tok {
		sp.modes[compact] = true
	}
}

func (sp modeSpec) matchesProto(id string) bool {
	if len(sp.protos) == 0 {
		return false
	}
	if sp.protos["usp"] && strings.HasSuffix(id, "_usp") {
		return true
	}
	if sp.protos["ax100"] && strings.Contains(id, "_ax100") {
		return true
	}
	if sp.protos["g3ruh"] && (strings.HasSuffix(id, "_g3ruh") || id == "gfsk9600" || id == "gmsk9600") {
		return true
	}
	if sp.protos["geoscan"] && id == demodGEOSCAN {
		return true
	}
	if sp.protos["ccsds"] && id == "qpsk_ccsds" {
		return true
	}
	return false
}

func specMatchesProfile(sp modeSpec, m ProfileMeta) bool {
	if !specModeMatches(sp, m) {
		return false
	}
	if m.BaudFlexible {
		return true
	}
	if m.Baud > 0 {
		if len(sp.bauds) == 0 {
			return false
		}
		return sp.bauds[roundBaud(m.Baud)]
	}
	// Голос, CW и SSTV: при явно заданной цифровой скорости в аргументе не подходят.
	// В составных строках AMSAT (argBaud == 0, скорости указаны внутри строки) допускаются.
	return sp.argBaud == 0
}

func specModeMatches(sp modeSpec, m ProfileMeta) bool {
	for _, s := range m.SatNOGSModes {
		norm := normalizeMode(s)
		if sp.modes[norm] {
			return true
		}
		compact := strings.NewReplacer(".", "", "-", "").Replace(norm)
		if sp.modes[compact] {
			return true
		}
	}
	// В реестре для AX.100 часто пишут «FSK AX.100 Mode 5» или «MSK AX.100 Mode 5»:
	// физически FSK/GFSK/GMSK/MSK принимаются одним FSK-демодулятором.
	if sp.protos["ax100"] && strings.Contains(m.ID, "_ax100") &&
		(sp.modes["FSK"] || sp.modes["GFSK"] || sp.modes["GMSK"] || sp.modes["MSK"] || sp.modes["2FSK"]) {
		return true
	}
	if sp.protos["geoscan"] && m.ID == demodGEOSCAN {
		return true
	}
	return false
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
