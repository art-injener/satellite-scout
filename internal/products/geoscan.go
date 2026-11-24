package products

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// GeoscanOffsetBase — смещение первого куска в заголовке кадра (ADR-009 §7.4).
	GeoscanOffsetBase = 0x8000
	// GeoscanChunkStep — шаг смещения / типичная длина данных куска.
	GeoscanChunkStep = 56
)

// PutGeoscanChunk дописывает кусок JPEG по смещению кадра (база 0x8000).
// Дыры заполняются нулями. При появлении EOI (FF D9) продукт сохраняется на диск.
func (s *Store) PutGeoscanChunk(m Meta, offset int, chunk []byte) Product {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.Kind == "" {
		m.Kind = KindGEOSCAN
	}
	if m.Mode == "" {
		m.Mode = "geoscan"
	}
	if m.ModeName == "" {
		m.ModeName = "GEOSCAN"
	}
	fileOff := offset - GeoscanOffsetBase
	if fileOff < 0 || len(chunk) == 0 {
		return Product{}
	}
	idx := fileOff / GeoscanChunkStep

	e := s.activeGeoscan(m.RadioPath)
	if e == nil {
		now := s.now().UTC()
		id := now.Format(idTime) + "-" + m.Mode
		for n := 2; s.find(id) != nil; n++ {
			id = fmt.Sprintf("%s-%s_%d", now.Format(idTime), m.Mode, n)
		}
		e = &entry{
			Product: Product{
				Meta: m, ID: id, StartedMs: uint64(now.UnixMilli()),
			},
			got: make(map[int]bool),
		}
		s.push(e)
	} else {
		if m.NoradID > 0 {
			e.NoradID = m.NoradID
		}
		if m.FrequencyHz > 0 {
			e.FrequencyHz = m.FrequencyHz
		}
	}

	need := fileOff + len(chunk)
	if len(e.jpeg) < need {
		grown := make([]byte, need)
		copy(grown, e.jpeg)
		e.jpeg = grown
	}
	copy(e.jpeg[fileOff:], chunk)
	e.got[idx] = true
	e.Chunks = len(e.got)
	e.Lines = e.filledBytes()
	e.Height = len(e.jpeg)
	e.Gaps = e.gapCount()

	if bytes.Contains(e.jpeg, []byte{0xFF, 0xD9}) {
		s.finishGeoscanLocked(e)
	}
	return e.Product
}

func (s *Store) activeGeoscan(pathID string) *entry {
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if e.Kind == KindGEOSCAN && !e.Done && e.jpeg != nil &&
			(pathID == "" || e.RadioPath == pathID) {
			return e
		}
	}
	return nil
}

func (e *entry) filledBytes() int {
	n := 0
	for idx := range e.got {
		start := idx * GeoscanChunkStep
		end := start + GeoscanChunkStep
		if end > len(e.jpeg) {
			end = len(e.jpeg)
		}
		if start < end {
			n += end - start
		}
	}
	return n
}

func (e *entry) gapCount() int {
	if len(e.got) == 0 || len(e.jpeg) == 0 {
		return 0
	}
	maxIdx := (len(e.jpeg) - 1) / GeoscanChunkStep
	gaps := 0
	for i := 0; i <= maxIdx; i++ {
		if !e.got[i] {
			gaps++
		}
	}
	return gaps
}

func (s *Store) finishGeoscanLocked(e *entry) {
	if e == nil || e.Done || e.jpeg == nil {
		return
	}
	path := filepath.Join(s.dir, e.ID+".jpg")
	if err := os.WriteFile(path, e.jpeg, 0o644); err != nil {
		return
	}
	e.jpeg = nil
	e.got = nil
	e.Done = true
	e.FinishedMs = uint64(s.now().UnixMilli())
}

// FinishGeoscan сохраняет текущий буфер без EOI (обрыв приёма).
func (s *Store) FinishGeoscan(id string) (Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.find(id)
	if e == nil || e.Kind != KindGEOSCAN {
		return Product{}, fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	if e.Done {
		return e.Product, nil
	}
	if e.jpeg == nil {
		return Product{}, fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	s.finishGeoscanLocked(e)
	return e.Product, nil
}
