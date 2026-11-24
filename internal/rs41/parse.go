package rs41

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Telemetry — разобранные поля кадра RS41 для колонки «Данные» / модалки.
type Telemetry struct {
	ID      string
	FrameNb uint16
	LatDeg  float64
	LonDeg  float64
	AltM    float64
	TempC   float64
	HasPos  bool
	HasTemp bool
}

// Parse извлекает поля кадра, копит калибровку в defaultCal и считает T при готовности.
func Parse(frame []byte) (Telemetry, bool) {
	return ParseCal(defaultCal, frame)
}

// ParseCal — как Parse, но со своим хранилищем калибровки.
func ParseCal(cal *CalStore, frame []byte) (Telemetry, bool) {
	if !FrameOK(frame) {
		return Telemetry{}, false
	}
	t := Telemetry{
		FrameNb: binary.LittleEndian.Uint16(frame[posFrameNb:]),
		ID:      sondeID(frame[posSondeID : posSondeID+8]),
	}
	if cal != nil {
		cal.Ingest(t.ID, frame)
		if temp, ok := cal.TemperatureC(t.ID, frame); ok {
			t.TempC, t.HasTemp = temp, true
		}
	}
	if checkTLV(frame, posGPS3) {
		x := int32(binary.LittleEndian.Uint32(frame[posECEFX:]))
		y := int32(binary.LittleEndian.Uint32(frame[posECEFX+4:]))
		z := int32(binary.LittleEndian.Uint32(frame[posECEFX+8:]))
		if x != 0 || y != 0 || z != 0 {
			lat, lon, alt := ecefToLLA(float64(x)/100, float64(y)/100, float64(z)/100)
			t.LatDeg, t.LonDeg, t.AltM = lat, lon, alt
			t.HasPos = true
		}
	}
	return t, true
}

func sondeID(b []byte) string {
	n := len(b)
	for n > 0 && (b[n-1] == 0 || b[n-1] == ' ') {
		n--
	}
	for _, c := range b[:n] {
		if c < 0x20 || c > 0x7e {
			return fmt.Sprintf("%x", b)
		}
	}
	return string(b[:n])
}

// ecefToLLA — ECEF (м) → WGS-84 (градусы, м). Итерация Bowring.
func ecefToLLA(x, y, z float64) (latDeg, lonDeg, altM float64) {
	const a = 6378137.0
	const f = 1 / 298.257223563
	b := a * (1 - f)
	e2 := 1 - (b*b)/(a*a)
	ep2 := (a*a)/(b*b) - 1
	p := math.Hypot(x, y)
	th := math.Atan2(z*a, p*b)
	lon := math.Atan2(y, x)
	lat := math.Atan2(z+ep2*b*math.Pow(math.Sin(th), 3), p-e2*a*math.Pow(math.Cos(th), 3))
	n := a / math.Sqrt(1-e2*math.Sin(lat)*math.Sin(lat))
	alt := p/math.Cos(lat) - n
	return lat * 180 / math.Pi, lon * 180 / math.Pi, alt
}
