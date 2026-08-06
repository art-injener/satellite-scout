package dsp

import (
	"encoding/binary"
	"math"
	"time"
	"unsafe"
)

// Константы SSTM wire protocol (ADR-002).
const (
	HeaderSize = 40
	Magic      = 0x4D545353 // "SSTM" в little-endian
	Version    = 1
)

// DataType — тип полезной нагрузки SSTM-фрейма.
type DataType uint8

const (
	DataTypeFFTDB     DataType = 0x01
	DataTypeIQC64     DataType = 0x02
	DataTypePCMI16    DataType = 0x03
	DataTypeIQI8      DataType = 0x04
	DataTypeIQI16     DataType = 0x05
	DataTypeHeartbeat DataType = 0xFE
)

// FrameHeader — заголовок SSTM-фрейма (40 байт, все поля little-endian).
type FrameHeader struct {
	DataType    DataType
	DisplayMode DisplayMode
	TimestampMs int64
	CenterHz    int64
	SpanHz      int64
	PayloadLen  uint32
	Seq         uint32
}

// EncodeHeader записывает 40-байтный SSTM-заголовок в buf.
// buf должен быть >= HeaderSize.
func EncodeHeader(buf []byte, h FrameHeader) {
	_ = buf[HeaderSize-1] // bounds check hint

	binary.LittleEndian.PutUint32(buf[0:4], Magic)
	buf[4] = Version
	buf[5] = byte(h.DataType)
	buf[6] = byte(h.DisplayMode)
	buf[7] = 0 // reserved
	binary.LittleEndian.PutUint64(buf[8:16], uint64(h.TimestampMs))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(h.CenterHz))
	binary.LittleEndian.PutUint64(buf[24:32], uint64(h.SpanHz))
	binary.LittleEndian.PutUint32(buf[32:36], h.PayloadLen)
	binary.LittleEndian.PutUint32(buf[36:40], h.Seq)
}

// Float32ToBytes преобразует []float32 в []byte без копирования.
// Результат валиден пока жив исходный слайс.
func Float32ToBytes(f []float32) []byte {
	if len(f) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&f[0])), len(f)*4)
}

// EncodeHeartbeat записывает heartbeat-фрейм (заголовок без payload).
func EncodeHeartbeat(buf []byte, seq uint32, centerHz, spanHz int64) {
	EncodeHeader(buf, FrameHeader{
		DataType:    DataTypeHeartbeat,
		TimestampMs: nowMs(),
		CenterHz:    centerHz,
		SpanHz:      spanHz,
		Seq:         seq,
	})
}

// nowMs — текущее время в миллисекундах UTC. Вынесена для подмены в тестах.
var nowMs = defaultNowMs

func defaultNowMs() int64 {
	return time.Now().UnixMilli()
}

// PayloadSize возвращает размер payload для N float32 бинов.
func PayloadSize(bins int) uint32 {
	return uint32(bins) * uint32(unsafe.Sizeof(float32(0)))
}

// MaxDBFS возвращает максимум из спектра (для быстрой оценки уровня).
func MaxDBFS(bins []float32) float32 {
	if len(bins) == 0 {
		return DBFloor
	}
	m := float32(math.Inf(-1))
	for _, v := range bins {
		if v > m {
			m = v
		}
	}
	return m
}
