package rs41

import (
	"encoding/binary"
	"math"
	"testing"
)

func putFloat32(dst []byte, v float32) {
	binary.LittleEndian.PutUint32(dst, math.Float32bits(v))
}

// buildCalFrames — субкадры 0x03..0x06 с коэффициентами для getT.
func buildCalFrames(t *testing.T, rf1, rf2 float32, co, calT [3]float32) map[int][]byte {
	t.Helper()
	flat := make([]byte, calFrames*calFrameLen)
	putFloat32(flat[61:], rf1)
	putFloat32(flat[65:], rf2)
	putFloat32(flat[77:], co[0])
	putFloat32(flat[81:], co[1])
	putFloat32(flat[85:], co[2])
	putFloat32(flat[89:], calT[0])
	putFloat32(flat[93:], calT[1])
	putFloat32(flat[97:], calT[2])
	out := make(map[int][]byte)
	for fr := 0x03; fr <= 0x06; fr++ {
		out[fr] = append([]byte(nil), flat[fr*calFrameLen:(fr+1)*calFrameLen]...)
	}
	return out
}

func TestCalStore_TemperatureFromSyntheticCal(t *testing.T) {
	cal := NewCalStore()
	id := "K1930293"
	rf1, rf2 := float32(750), float32(1100)
	co := [3]float32{-243.911, 0.187654, 8.2e-6}
	calT := [3]float32{1.0, 0.0, 0.0}
	for fr, data := range buildCalFrames(t, rf1, rf2, co, calT) {
		cal.PutFrame(id, fr, data)
	}
	if !cal.ReadyT(id) {
		t.Fatal("ReadyT")
	}

	frame := append([]byte(nil), goldenClear(t)...)
	// Подставить известные измерительные f,f1,f2 в PTU (uint24 LE) и пересчитать CRC.
	f, f1, f2 := uint32(400000), uint32(300000), uint32(500000)
	putU24(frame[posPTU+2:], f)
	putU24(frame[posPTU+5:], f1)
	putU24(frame[posPTU+8:], f2)
	fixPTUCRC(frame)

	want := getT(f, f1, f2, ptuCal{rf1: rf1, rf2: rf2, co1: co, calT1: calT})
	got, ok := cal.TemperatureC(id, frame)
	if !ok {
		t.Fatal("TemperatureC")
	}
	if math.Abs(got-want) > 1e-3 {
		t.Fatalf("T=%.6f want %.6f", got, want)
	}
	tel, ok := ParseCal(cal, frame)
	if !ok || !tel.HasTemp || math.Abs(tel.TempC-want) > 1e-3 {
		t.Fatalf("tel=%+v", tel)
	}
}

func putU24(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
}

func fixPTUCRC(frame []byte) {
	n := int(frame[posPTU+1])
	body := frame[posPTU+2 : posPTU+2+n]
	sum := CRC16(body)
	frame[posPTU+2+n] = byte(sum)
	frame[posPTU+3+n] = byte(sum >> 8)
}

func TestCalStore_IngestFromStatus(t *testing.T) {
	cal := NewCalStore()
	frame := append([]byte(nil), goldenClear(t)...)
	id := sondeID(frame[posSondeID : posSondeID+8])
	cal.Ingest(id, frame)
	fr := int(frame[posCalData])
	cal.mu.Lock()
	have := cal.byID[id].have[fr]
	cal.mu.Unlock()
	if !have {
		t.Fatalf("subframe %d not stored", fr)
	}
}
