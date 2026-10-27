package sigmf

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
)

// Dataset — открытая запись SigMF: meta + поток отсчётов в формате .cfile (cf32_le).
// io.ReadCloser: Read отдаёт байты float32 I,Q little-endian; Close закрывает файл данных.
type Dataset struct {
	Meta Meta

	file     *os.File
	datatype string
}

// Open открывает пару .sigmf-meta / .sigmf-data по пути к meta.
func Open(metaPath string) (*Dataset, error) {
	meta, err := LoadMeta(metaPath)
	if err != nil {
		return nil, err
	}
	dp := dataPath(metaPath)
	f, err := os.Open(dp)
	if err != nil {
		return nil, fmt.Errorf("sigmf: данные %s: %w", dp, err)
	}
	return &Dataset{Meta: meta, file: f, datatype: meta.Global.Datatype}, nil
}

// SampleRate — частота дискретизации записи, Гц.
func (d *Dataset) SampleRate() uint32 { return d.Meta.SampleRate() }

// CenterHz — частота центра первого capture, Гц.
func (d *Dataset) CenterHz() uint64 { return d.Meta.CenterHz() }

// Close закрывает файл данных.
func (d *Dataset) Close() error {
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

// Seek перематывает файл данных (нужно имитатору для повторного и циклического replay).
func (d *Dataset) Seek(offset int64, whence int) (int64, error) {
	if d.file == nil {
		return 0, io.ErrClosedPipe
	}
	return d.file.Seek(offset, whence)
}

// Read читает отсчёты и отдаёт их как cf32_le (совместимо с имитатором .cfile).
func (d *Dataset) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	outN := len(p) - len(p)%8
	if outN == 0 {
		return 0, nil
	}
	switch d.datatype {
	case DatatypeCF32LE:
		n, err := d.file.Read(p[:outN])
		n -= n % 8
		if n == 0 {
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
		return n, err
	case DatatypeCI16LE:
		return d.readCI16AsCF32(p[:outN])
	default:
		return 0, fmt.Errorf("sigmf: datatype %q", d.datatype)
	}
}

func (d *Dataset) readCI16AsCF32(out []byte) (int, error) {
	samples := len(out) / 8
	in := make([]byte, samples*4)
	n, err := io.ReadFull(d.file, in)
	if n < 4 {
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}
	n -= n % 4
	nOut := 0
	for off := 0; off+4 <= n; off += 4 {
		re := float32(int16(binary.LittleEndian.Uint16(in[off:]))) / 32768
		im := float32(int16(binary.LittleEndian.Uint16(in[off+2:]))) / 32768
		binary.LittleEndian.PutUint32(out[nOut:], math.Float32bits(re))
		binary.LittleEndian.PutUint32(out[nOut+4:], math.Float32bits(im))
		nOut += 8
	}
	if err == io.ErrUnexpectedEOF {
		err = nil
	}
	return nOut, err
}
