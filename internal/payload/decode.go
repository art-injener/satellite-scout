package payload

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"unicode"

	"github.com/art-injener/satellite-scout/internal/ax100"
	"github.com/art-injener/satellite-scout/internal/ax25"
	"github.com/art-injener/satellite-scout/internal/geoscan"
	"github.com/art-injener/satellite-scout/internal/m10"
	"github.com/art-injener/satellite-scout/internal/rs41"
	"github.com/art-injener/satellite-scout/internal/usp"
)

const (
	profileGEOSCAN = "fsk9600_geoscan"

	// Заголовок куска снимка GEOSCAN-EDELVEIS (ADR-009 §7.4).
	imgMagic0     = 0x01
	imgMagic2     = 0x3E
	imgMagic4     = 0x09
	imgMagic7     = 0x0B
	imgHeaderLen  = 8
	imgChunkStep  = 56
	imgOffsetBase = 0x8000
)

// Decode разбирает кадр по ярусам: профиль → общие → unknown (ADR-009 §7.3).
// Ярус по NORAD — позже, вместе с привязкой кадра к КА.
func Decode(f Frame) Payload {
	if len(f.Raw) == 0 {
		return Unknown(nil)
	}
	for _, d := range profileDecoders(f.Profile) {
		if p, ok := d.Decode(f); ok {
			return p
		}
	}
	for _, d := range generalDecoders() {
		if p, ok := d.Decode(f); ok {
			return p
		}
	}
	if isLineProfile(f.Profile) {
		return Line(f.Raw)
	}
	return Unknown(f.Raw)
}

// isLineProfile — профили без кадрового CRC: пачка бит → «Линия» + hex.
func isLineProfile(id string) bool {
	switch id {
	case "ook", "msk9600", "3fsk4800", "4fsk4800", "gmfsk4800",
		"pi4_dqpsk", "apsk16", "fsk9600":
		return true
	default:
		return false
	}
}

func profileDecoders(profile string) []Decoder {
	switch profile {
	case profileGEOSCAN:
		return []Decoder{geoscanImage{}, geoscanBeacon{}}
	case "gmsk4800_ax100", "gmsk9600_ax100", "fsk4800_ax100",
		"gmsk9600_ax100_rs", "fsk9600_ax100_rs":
		return []Decoder{suchaiBeacon{}, cspFrame{}}
	case "gmsk2400_usp", "gmsk4800_usp", "gmsk9600_usp":
		return []Decoder{uspBeacon{}}
	case "rs41":
		return []Decoder{rs41Frame{}}
	case "m10":
		return []Decoder{m10Frame{}}
	default:
		return nil
	}
}

// m10Frame — телеметрия Meteomodem M10 после checksum.
type m10Frame struct{}

func (m10Frame) ID() string { return "m10" }

func (m10Frame) Decode(f Frame) (Payload, bool) {
	tel, ok := m10.Parse(f.Raw)
	if !ok {
		return Payload{}, false
	}
	p := Payload{
		Kind:    KindSonde,
		Decoder: "m10",
		Summary: Label(KindSonde) + " · " + tel.ID,
		Src:     tel.ID,
		InfoHex: hex.EncodeToString(f.Raw),
		Params: []Param{
			{ID: "sonde_id", Name: "ID", Value: tel.ID, Key: true},
		},
	}
	if tel.HasTemp {
		p.Params = append(p.Params,
			Param{ID: "temp", Name: "Температура", Value: fmt.Sprintf("%.1f", tel.TempC), Unit: "°C", Key: true},
		)
	}
	if tel.HasPos {
		p.Params = append(p.Params,
			Param{ID: "lat", Name: "Широта", Value: fmt.Sprintf("%.5f", tel.LatDeg), Unit: "°", Key: true},
			Param{ID: "lon", Name: "Долгота", Value: fmt.Sprintf("%.5f", tel.LonDeg), Unit: "°", Key: true},
			Param{ID: "alt", Name: "Высота", Value: fmt.Sprintf("%.0f", tel.AltM), Unit: "м", Key: true},
		)
	}
	return p, true
}

// rs41Frame — телеметрия Vaisala RS41 после dual RS и CRC статуса.
type rs41Frame struct{}

func (rs41Frame) ID() string { return "rs41" }

func (rs41Frame) Decode(f Frame) (Payload, bool) {
	tel, ok := rs41.Parse(f.Raw)
	if !ok {
		return Payload{}, false
	}
	p := Payload{
		Kind:    KindSonde,
		Decoder: "rs41",
		Summary: Label(KindSonde) + " · " + tel.ID,
		Src:     tel.ID,
		InfoHex: hex.EncodeToString(f.Raw),
		Params: []Param{
			{ID: "sonde_id", Name: "ID", Value: tel.ID, Key: true},
			{ID: "frame", Name: "Кадр", Value: fmt.Sprintf("%d", tel.FrameNb)},
		},
	}
	if tel.HasTemp {
		p.Params = append(p.Params,
			Param{ID: "temp", Name: "Температура", Value: fmt.Sprintf("%.1f", tel.TempC), Unit: "°C", Key: true},
		)
	}
	if tel.HasPos {
		p.Params = append(p.Params,
			Param{ID: "lat", Name: "Широта", Value: fmt.Sprintf("%.5f", tel.LatDeg), Unit: "°", Key: true},
			Param{ID: "lon", Name: "Долгота", Value: fmt.Sprintf("%.5f", tel.LonDeg), Unit: "°", Key: true},
			Param{ID: "alt", Name: "Высота", Value: fmt.Sprintf("%.0f", tel.AltM), Unit: "м", Key: true},
		)
	}
	return p, true
}

// uspBeacon — маяк SPUTNIX USP (0x4216 / 0x4246) внутри AX.25.
type uspBeacon struct{}

func (uspBeacon) ID() string { return "usp.beacon" }

func (uspBeacon) Decode(f Frame) (Payload, bool) {
	params, kind, ok := usp.BeaconParams(f.Raw)
	if !ok {
		return Payload{}, false
	}
	out := make([]Param, len(params))
	for i, p := range params {
		out[i] = Param{ID: p.ID, Name: p.Name, Value: p.Value, Unit: p.Unit, Key: p.Key}
	}
	src, dest, _ := usp.CallsignsFromRaw(f.Raw)
	label := "USP"
	switch kind {
	case "uhf_beacon":
		label = "USP УКВ"
	case "extended_beacon":
		label = "USP расшир."
	}
	sum := Label(KindBeacon) + " · " + label
	if src != "" {
		sum = Label(KindBeacon) + " · " + src
	}
	return Payload{
		Kind:    KindBeacon,
		Decoder: "usp.beacon",
		Summary: sum,
		Src:     src,
		Dest:    dest,
		InfoHex: hex.EncodeToString(f.Raw),
		Params:  out,
	}, true
}

// suchaiBeacon — статусный маяк SUCHAI-2/3 / PlantSat после CSP (satnogs suchai2.ksy).
type suchaiBeacon struct{}

func (suchaiBeacon) ID() string { return "suchai.beacon" }

func (suchaiBeacon) Decode(f Frame) (Payload, bool) {
	params, sat, ok := ax100.SuchaiBeaconParams(f.Raw)
	if !ok {
		return Payload{}, false
	}
	out := make([]Param, len(params))
	for i, p := range params {
		out[i] = Param{ID: p.ID, Name: p.Name, Value: p.Value, Unit: p.Unit, Key: p.Key}
	}
	h, _, _ := ax100.ParseCSP(f.Raw)
	sum := Label(KindBeacon) + " · " + sat
	return Payload{
		Kind:    KindBeacon,
		Decoder: "suchai.beacon",
		Summary: sum,
		Src:     fmt.Sprintf("%d:%d", h.Source, h.SrcPort),
		Dest:    fmt.Sprintf("%d:%d", h.Destination, h.DstPort),
		InfoHex: hex.EncodeToString(f.Raw),
		Params:  out,
	}, true
}

// cspFrame — Cubesat Space Protocol поверх AX.100 (после RS); запасной ярус без раскладки КА.
type cspFrame struct{}

func (cspFrame) ID() string { return "csp" }

func (cspFrame) Decode(f Frame) (Payload, bool) {
	h, body, ok := ax100.ParseCSP(f.Raw)
	if !ok {
		return Payload{}, false
	}
	sum := ax100.FormatCSP(h)
	info := string(bytes.TrimRight(body, "\x00"))
	p := Payload{
		Kind:    KindBeacon,
		Decoder: "csp",
		Summary: sum,
		Src:     fmt.Sprintf("%d:%d", h.Source, h.SrcPort),
		Dest:    fmt.Sprintf("%d:%d", h.Destination, h.DstPort),
		InfoHex: hex.EncodeToString(body),
		Params: []Param{
			{ID: "csp_src", Name: "Источник", Value: fmt.Sprintf("%d", h.Source), Key: true},
			{ID: "csp_dst", Name: "Назначение", Value: fmt.Sprintf("%d", h.Destination), Key: true},
			{ID: "csp_sport", Name: "Порт ист.", Value: fmt.Sprintf("%d", h.SrcPort)},
			{ID: "csp_dport", Name: "Порт назн.", Value: fmt.Sprintf("%d", h.DstPort), Key: true},
		},
	}
	if printable(body) && len(bytes.TrimSpace(body)) > 0 {
		p.Kind = KindText
		p.Info = info
		p.Summary = sum + " · " + truncate(info, 32)
	}
	return p, true
}

func generalDecoders() []Decoder {
	return []Decoder{ax25UI{}}
}

// geoscanImage — кусок снимка по заголовку §7.4.
type geoscanImage struct{}

func (geoscanImage) ID() string { return "geoscan.image" }

func (geoscanImage) Decode(f Frame) (Payload, bool) {
	raw := f.Raw
	if len(raw) < imgHeaderLen {
		return Payload{}, false
	}
	if raw[0] != imgMagic0 || raw[1] != 0x00 || raw[2] != imgMagic2 {
		return Payload{}, false
	}
	if raw[4] != imgMagic4 || raw[7] != imgMagic7 {
		return Payload{}, false
	}
	off := int(binary.LittleEndian.Uint16(raw[5:7]))
	chunk := append([]byte(nil), raw[imgHeaderLen:]...)
	n := 0
	if off >= imgOffsetBase {
		n = (off-imgOffsetBase)/imgChunkStep + 1
	}
	sum := fmt.Sprintf("Снимок: кусок %d, смещение %d", n, off)
	return Payload{
		Kind:     KindImage,
		Decoder:  "geoscan.image",
		Summary:  sum,
		InfoHex:  hex.EncodeToString(raw),
		Offset:   off,
		Chunk:    chunk,
	}, true
}

// geoscanBeacon — маяк платформы: AX.25 с получателем BEACON, в т.ч. после транспортного заголовка.
type geoscanBeacon struct{}

func (geoscanBeacon) ID() string { return "geoscan.beacon" }

func (geoscanBeacon) Decode(f Frame) (Payload, bool) {
	frame, ok := findAX25(f.Raw)
	if !ok {
		return Payload{}, false
	}
	params, ok := geoscan.BeaconParams(frame)
	if !ok {
		return Payload{}, false
	}
	out := make([]Param, len(params))
	for i, p := range params {
		out[i] = Param{ID: p.ID, Name: p.Name, Value: p.Value, Unit: p.Unit, Key: p.Key}
	}
	src, dest := callsign(frame.Src), callsign(frame.Dest)
	return Payload{
		Kind:    KindBeacon,
		Decoder: "geoscan.beacon",
		Summary: Label(KindBeacon) + " · " + src,
		Src:     src,
		Dest:    dest,
		InfoHex: hex.EncodeToString(frame.Info),
		Params:  out,
	}, true
}

// ax25UI — строгий UI-кадр: позывные, control/PID; печатный текст → message, иначе unknown не ставим здесь.
type ax25UI struct{}

func (ax25UI) ID() string { return "ax25.ui" }

func (ax25UI) Decode(f Frame) (Payload, bool) {
	frame, ok := parseStrict(f.Raw)
	if !ok {
		return Payload{}, false
	}
	src, dest := callsign(frame.Src), callsign(frame.Dest)
	info := bytes.TrimRight(frame.Info, "\x00")
	p := Payload{
		Src:     src,
		Dest:    dest,
		InfoHex: hex.EncodeToString(frame.Info),
	}
	if printable(info) && len(info) > 0 {
		p.Kind = KindText
		p.Decoder = "ax25.ui"
		p.Info = string(info)
		p.Summary = truncate(p.Info, 48)
		return p, true
	}
	// Адресное поле верное, содержимое не текст — не выдаём как ТМИ.
	return Payload{}, false
}

func findAX25(raw []byte) (ax25.Frame, bool) {
	if f, ok := parseStrict(raw); ok {
		return f, true
	}
	if f, ok := parseStrict(repairAddressEnd(raw)); ok {
		return f, true
	}
	// Транспортный заголовок перед AX.25 (несколько байт в начале кадра GEOSCAN).
	for off := 1; off <= 8 && off < len(raw); off++ {
		body := raw[off:]
		if f, ok := parseStrict(body); ok {
			return f, true
		}
		// В эфире у источника часто сброшен бит конца адресного поля — чиним копию.
		if f, ok := parseStrict(repairAddressEnd(body)); ok {
			return f, true
		}
	}
	return ax25.Frame{}, false
}

// repairAddressEnd выставляет бит конца у второго адреса, если оба SSID-байта без него.
// Иначе Parse ждёт диджипитер и принимает control за позывной.
func repairAddressEnd(b []byte) []byte {
	const addrBytes = 14
	if len(b) < addrBytes {
		return b
	}
	if b[6]&0x01 != 0 || b[13]&0x01 != 0 {
		return b
	}
	out := append([]byte(nil), b...)
	out[13] |= 0x01
	return out
}

// parseStrict — UI-кадр с проверкой позывных и PID (ADR-009 §7.3).
func parseStrict(b []byte) (ax25.Frame, bool) {
	f, err := ax25.Parse(b)
	if err != nil {
		return ax25.Frame{}, false
	}
	if !validCall(f.Dest.Call) || !validCall(f.Src.Call) {
		return ax25.Frame{}, false
	}
	if f.Dest.SSID > 15 || f.Src.SSID > 15 {
		return ax25.Frame{}, false
	}
	// PID: в Parse уже отрезан control; повторно смотрим сырые байты.
	end := addressFieldEnd(b)
	if end < 0 || end+2 > len(b) {
		return ax25.Frame{}, false
	}
	if b[end+1] != 0xF0 { // без L3, как у UI-маяков
		return ax25.Frame{}, false
	}
	return f, true
}

func addressFieldEnd(b []byte) int {
	const callLen = 6
	for i := callLen; i < len(b) && i < 10*(callLen+1); i += callLen + 1 {
		if b[i]&0x01 != 0 {
			return i + 1
		}
	}
	return -1
}

func validCall(s string) bool {
	if s == "" || len(s) > 6 {
		return false
	}
	for _, c := range s {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func callsign(a ax25.Address) string {
	if a.SSID == 0 {
		return a.Call
	}
	return fmt.Sprintf("%s-%d", a.Call, a.SSID)
}

func printable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c > unicode.MaxASCII || (!unicode.IsPrint(rune(c)) && !unicode.IsSpace(rune(c))) {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
