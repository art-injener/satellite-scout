// Package wav читает звук из файлов RIFF/WAVE: PCM 8 и 16 бит и float32,
// моно и многоканальный (каналы усредняются).
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

// ErrFormat — файл не WAV или его формат не поддерживается.
var ErrFormat = errors.New("unsupported wav")

const (
	formatPCM   = 1
	formatFloat = 3
	// formatExtensible — формат задан в расширении fmt; первые 2 байта GUID — тот же код.
	formatExtensible = 0xFFFE
)

// File — звук файла: отсчёты в [-1, 1) и их частота.
type File struct {
	SampleRate int
	Samples    []float32
}

type format struct {
	code     uint16
	channels int
	rate     int
	bits     int
}

// Read разбирает WAV целиком.
func Read(r io.Reader) (File, error) {
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return File{}, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return File{}, fmt.Errorf("%w: no RIFF/WAVE header", ErrFormat)
	}
	var f *format
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return File{}, fmt.Errorf("%w: no data chunk", ErrFormat)
		}
		id, size := string(ch[0:4]), int64(binary.LittleEndian.Uint32(ch[4:8]))
		switch id {
		case "fmt ":
			b := make([]byte, size)
			if _, err := io.ReadFull(r, b); err != nil {
				return File{}, fmt.Errorf("%w: fmt: %v", ErrFormat, err)
			}
			parsed, err := parseFormat(b)
			if err != nil {
				return File{}, err
			}
			f = &parsed
		case "data":
			if f == nil {
				return File{}, fmt.Errorf("%w: data before fmt", ErrFormat)
			}
			b, err := io.ReadAll(io.LimitReader(r, size))
			if err != nil {
				return File{}, err
			}
			return File{SampleRate: f.rate, Samples: decode(*f, b)}, nil
		default:
			if _, err := io.CopyN(io.Discard, r, size); err != nil {
				return File{}, fmt.Errorf("%w: chunk %q: %v", ErrFormat, id, err)
			}
		}
		if size%2 == 1 {
			if _, err := io.CopyN(io.Discard, r, 1); err != nil {
				return File{}, fmt.Errorf("%w: no data chunk", ErrFormat)
			}
		}
	}
}

func parseFormat(b []byte) (format, error) {
	if len(b) < 16 {
		return format{}, fmt.Errorf("%w: short fmt", ErrFormat)
	}
	f := format{
		code:     binary.LittleEndian.Uint16(b[0:2]),
		channels: int(binary.LittleEndian.Uint16(b[2:4])),
		rate:     int(binary.LittleEndian.Uint32(b[4:8])),
		bits:     int(binary.LittleEndian.Uint16(b[14:16])),
	}
	if f.code == formatExtensible && len(b) >= 26 {
		f.code = binary.LittleEndian.Uint16(b[24:26])
	}
	ok := f.channels > 0 && f.rate > 0 &&
		(f.code == formatPCM && (f.bits == 8 || f.bits == 16) || f.code == formatFloat && f.bits == 32)
	if !ok {
		return format{}, fmt.Errorf("%w: format %d, %d bit", ErrFormat, f.code, f.bits)
	}
	return f, nil
}

func decode(f format, b []byte) []float32 {
	width := f.bits / 8
	frames := len(b) / (width * f.channels)
	out := make([]float32, frames)
	for i := range out {
		var sum float32
		for c := range f.channels {
			p := b[(i*f.channels+c)*width:]
			switch {
			case f.code == formatFloat:
				sum += math.Float32frombits(binary.LittleEndian.Uint32(p))
			case width == 1:
				sum += (float32(p[0]) - 128) / 128 // 8-битный PCM — без знака
			default:
				sum += float32(int16(binary.LittleEndian.Uint16(p))) / 32768
			}
		}
		out[i] = sum / float32(f.channels)
	}
	return out
}
