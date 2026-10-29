package wav

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// build assembles a RIFF/WAVE file with the given format and raw sample data.
func build(format, channels uint16, rate uint32, bits uint16, data []byte, extra ...[]byte) []byte {
	var fmtChunk bytes.Buffer
	align := channels * bits / 8
	for _, v := range []any{format, channels, rate, rate * uint32(align), align, bits} {
		_ = binary.Write(&fmtChunk, binary.LittleEndian, v)
	}
	var body bytes.Buffer
	body.WriteString("WAVE")
	chunk := func(id string, b []byte) {
		body.WriteString(id)
		_ = binary.Write(&body, binary.LittleEndian, uint32(len(b)))
		body.Write(b)
		if len(b)%2 == 1 {
			body.WriteByte(0)
		}
	}
	chunk("fmt ", fmtChunk.Bytes())
	for _, e := range extra {
		chunk("LIST", e)
	}
	chunk("data", data)
	var out bytes.Buffer
	out.WriteString("RIFF")
	_ = binary.Write(&out, binary.LittleEndian, uint32(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

func le16(vs ...int16) []byte {
	var b bytes.Buffer
	for _, v := range vs {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-4 }

func TestRead_PCM16Mono(t *testing.T) {
	f, err := Read(bytes.NewReader(build(1, 1, 11025, 16, le16(0, 16384, -32768, 32767))))
	if err != nil {
		t.Fatal(err)
	}
	if f.SampleRate != 11025 || len(f.Samples) != 4 {
		t.Fatalf("rate=%d n=%d", f.SampleRate, len(f.Samples))
	}
	want := []float32{0, 0.5, -1, 32767.0 / 32768}
	for i, w := range want {
		if !near(f.Samples[i], w) {
			t.Errorf("sample %d = %v, want %v", i, f.Samples[i], w)
		}
	}
}

func TestRead_StereoTakesTheMean(t *testing.T) {
	// A LIST chunk before data must be skipped.
	f, err := Read(bytes.NewReader(build(1, 2, 44100, 16, le16(16384, 0, -16384, -16384), []byte("INFOabc"))))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Samples) != 2 || !near(f.Samples[0], 0.25) || !near(f.Samples[1], -0.5) {
		t.Errorf("samples=%v, want [0.25 -0.5]", f.Samples)
	}
}

func TestRead_PCM8AndFloat(t *testing.T) {
	f, err := Read(bytes.NewReader(build(1, 1, 8000, 8, []byte{128, 255, 0})))
	if err != nil {
		t.Fatal(err)
	}
	if !near(f.Samples[0], 0) || !near(f.Samples[1], 127.0/128) || !near(f.Samples[2], -1) {
		t.Errorf("8-bit samples=%v", f.Samples)
	}

	var data bytes.Buffer
	_ = binary.Write(&data, binary.LittleEndian, []float32{0.25, -0.75})
	f, err = Read(bytes.NewReader(build(3, 1, 48000, 32, data.Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	if !near(f.Samples[0], 0.25) || !near(f.Samples[1], -0.75) {
		t.Errorf("float samples=%v", f.Samples)
	}
}

func TestRead_Rejects(t *testing.T) {
	cases := map[string][]byte{
		"not riff":   []byte("RIFX0000WAVE"),
		"24-bit":     build(1, 1, 8000, 24, make([]byte, 6)),
		"compressed": build(2, 1, 8000, 4, make([]byte, 4)),
		"no data":    []byte("RIFF\x04\x00\x00\x00WAVE"),
	}
	for name, b := range cases {
		if _, err := Read(bytes.NewReader(b)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
