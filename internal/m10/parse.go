package m10

import (
	"fmt"
	"math"
)

// Telemetry — поля кадра M10 для Эфира.
type Telemetry struct {
	ID      string
	LatDeg  float64
	LonDeg  float64
	AltM    float64
	TempC   float64
	HasPos  bool
	HasTemp bool
}

// Parse разбирает кадр с верным checksum.
func Parse(frame []byte) (Telemetry, bool) {
	if !FrameOK(frame) {
		return Telemetry{}, false
	}
	t := Telemetry{ID: serialNumber(frame[posSN : posSN+5])}
	lat := float64(beInt32(frame[posLat:])) / b60b60
	lon := float64(beInt32(frame[posLon:])) / b60b60
	alt := float64(beInt32(frame[posAlt:])) / 1000.0
	if lat != 0 || lon != 0 {
		t.LatDeg, t.LonDeg, t.AltM = lat, lon, alt
		t.HasPos = true
	}
	if temp, ok := temperatureC(frame); ok {
		t.TempC, t.HasTemp = temp, true
	}
	return t, true
}

func beInt32(b []byte) int32 {
	v := int32(b[0])<<24 | int32(b[1])<<16 | int32(b[2])<<8 | int32(b[3])
	return v
}

func serialNumber(raw []byte) string {
	// Как get_SN в m10mod.c: "YMM X NNNNN".
	b2 := raw[2]
	head := fmt.Sprintf("%X%02d", (b2>>4)&0xF, b2&0xF)
	w := uint16(raw[3]) | uint16(raw[4])<<8
	return fmt.Sprintf("%s %X %d%04d", head, raw[0]&0xF, (w>>13)&0x7, w&0x1FFF)
}

// temperatureC — NTC Shibaura PB5-41E (Steinhart–Hart из m10mod.c).
func temperatureC(frame []byte) (float64, bool) {
	scT := frame[0x3E]
	adc := uint16(frame[0x40])<<8 | uint16(frame[0x3F])
	if adc < 0xA000 {
		return 0, false
	}
	adcRT := float64(adc - 0xA000)
	if adcRT <= 0 || scT > 2 {
		return 0, false
	}
	const (
		p0 = 1.07303516e-03
		p1 = 2.41296733e-04
		p2 = 2.26744154e-06
		p3 = 6.52855181e-08
		adcMax = 4095.0
	)
	rs := [...]float64{12.1e3, 36.5e3, 475.0e3}
	rp := [...]float64{1e20, 330.0e3, 2000.0e3}
	x := (adcMax - adcRT) / adcRT
	r := rs[scT] / (x - rs[scT]/rp[scT])
	if r <= 0 {
		return 0, false
	}
	lr := math.Log(r)
	tk := 1 / (p0 + p1*lr + p2*lr*lr + p3*lr*lr*lr)
	return tk - 273.15, true
}
