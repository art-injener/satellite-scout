package dsp

import (
	"encoding/binary"
	"testing"
)

func TestEncodeHeader_MagicAndVersion(t *testing.T) {
	buf := make([]byte, HeaderSize)
	EncodeHeader(buf, FrameHeader{DataType: DataTypeFFTDB})

	magic := binary.LittleEndian.Uint32(buf[0:4])
	if magic != Magic {
		t.Errorf("magic=0x%08X, want 0x%08X", magic, Magic)
	}
	if buf[4] != Version {
		t.Errorf("version=%d, want %d", buf[4], Version)
	}
}

func TestEncodeHeader_Fields(t *testing.T) {
	buf := make([]byte, HeaderSize)
	h := FrameHeader{
		DataType:    DataTypeFFTDB,
		DisplayMode: ModeMaxHold,
		TimestampMs: 1714065297280,
		CenterHz:    437365000,
		SpanHz:      192000,
		PayloadLen:  2048,
		Seq:         791,
	}
	EncodeHeader(buf, h)

	if buf[5] != 0x01 {
		t.Errorf("data_type=0x%02X, want 0x01", buf[5])
	}
	if buf[6] != 0x02 {
		t.Errorf("display_mode=0x%02X, want 0x02", buf[6])
	}
	if buf[7] != 0 {
		t.Errorf("reserved=%d, want 0", buf[7])
	}

	ts := int64(binary.LittleEndian.Uint64(buf[8:16]))
	if ts != 1714065297280 {
		t.Errorf("timestamp_ms=%d", ts)
	}

	center := int64(binary.LittleEndian.Uint64(buf[16:24]))
	if center != 437365000 {
		t.Errorf("center_hz=%d", center)
	}

	span := int64(binary.LittleEndian.Uint64(buf[24:32]))
	if span != 192000 {
		t.Errorf("span_hz=%d", span)
	}

	plen := binary.LittleEndian.Uint32(buf[32:36])
	if plen != 2048 {
		t.Errorf("payload_len=%d", plen)
	}

	seq := binary.LittleEndian.Uint32(buf[36:40])
	if seq != 791 {
		t.Errorf("seq=%d", seq)
	}
}

func TestEncodeHeartbeat(t *testing.T) {
	buf := make([]byte, HeaderSize)
	EncodeHeartbeat(buf, 42, 145800000, 48000)

	if buf[5] != byte(DataTypeHeartbeat) {
		t.Errorf("data_type=0x%02X, want 0xFE", buf[5])
	}
	plen := binary.LittleEndian.Uint32(buf[32:36])
	if plen != 0 {
		t.Errorf("heartbeat payload_len=%d, want 0", plen)
	}
	seq := binary.LittleEndian.Uint32(buf[36:40])
	if seq != 42 {
		t.Errorf("seq=%d, want 42", seq)
	}
}

func TestFloat32ToBytes(t *testing.T) {
	data := []float32{1.0, -2.0, 3.5}
	b := Float32ToBytes(data)
	if len(b) != 12 {
		t.Fatalf("len=%d, want 12", len(b))
	}
	// float32 1.0 в LE = 0x3F800000
	got := binary.LittleEndian.Uint32(b[0:4])
	if got != 0x3F800000 {
		t.Errorf("first float bytes=0x%08X, want 0x3F800000", got)
	}
}

func TestFloat32ToBytes_Nil(t *testing.T) {
	b := Float32ToBytes(nil)
	if b != nil {
		t.Errorf("nil input should return nil")
	}
}

func TestPayloadSize(t *testing.T) {
	if PayloadSize(512) != 2048 {
		t.Errorf("PayloadSize(512)=%d, want 2048", PayloadSize(512))
	}
}
