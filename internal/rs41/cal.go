package rs41

import (
	"encoding/binary"
	"math"
	"sync"
)

const (
	calFrames   = 51 // 0x00..0x32
	calFrameLen = 16
	posCalData  = 0x052
)

// CalStore копит 16-байтные калибровочные субкадры по ID зонда.
type CalStore struct {
	mu   sync.Mutex
	byID map[string]*calState
}

type calState struct {
	bytes [calFrames][calFrameLen]byte
	have  [calFrames]bool
}

// NewCalStore создаёт пустое хранилище калибровки.
func NewCalStore() *CalStore {
	return &CalStore{byID: make(map[string]*calState)}
}

// defaultCal — общий накопитель для Parse в эфире.
var defaultCal = NewCalStore()

// Ingest сохраняет субкадр из блока статуса (если CRC статуса уже проверен).
func (s *CalStore) Ingest(id string, frame []byte) {
	if s == nil || id == "" || len(frame) < posCalData+1+calFrameLen {
		return
	}
	fr := int(frame[posCalData])
	if fr < 0 || fr >= calFrames {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byID[id]
	if st == nil {
		st = &calState{}
		s.byID[id] = st
	}
	if st.have[fr] {
		return
	}
	copy(st.bytes[fr][:], frame[posCalData+1:posCalData+1+calFrameLen])
	st.have[fr] = true
}

// ReadyT — есть субкадры 0x03..0x06 для расчёта температуры.
func (s *CalStore) ReadyT(id string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byID[id]
	if st == nil {
		return false
	}
	return st.have[0x03] && st.have[0x04] && st.have[0x05] && st.have[0x06]
}

// PutFrame подставляет субкадр (для тестов и ручной подгрузки).
func (s *CalStore) PutFrame(id string, fr int, data []byte) {
	if s == nil || id == "" || fr < 0 || fr >= calFrames || len(data) < calFrameLen {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byID[id]
	if st == nil {
		st = &calState{}
		s.byID[id] = st
	}
	copy(st.bytes[fr][:], data[:calFrameLen])
	st.have[fr] = true
}

type ptuCal struct {
	rf1, rf2   float32
	co1        [3]float32
	calT1      [3]float32
}

func (s *CalStore) loadPTU(id string) (ptuCal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.byID[id]
	if st == nil || !(st.have[0x03] && st.have[0x04] && st.have[0x05] && st.have[0x06]) {
		return ptuCal{}, false
	}
	flat := make([]byte, calFrames*calFrameLen)
	for i := 0; i < calFrames; i++ {
		copy(flat[i*calFrameLen:], st.bytes[i][:])
	}
	var c ptuCal
	c.rf1 = math.Float32frombits(binary.LittleEndian.Uint32(flat[61:]))
	c.rf2 = math.Float32frombits(binary.LittleEndian.Uint32(flat[65:]))
	c.co1[0] = math.Float32frombits(binary.LittleEndian.Uint32(flat[77:]))
	c.co1[1] = math.Float32frombits(binary.LittleEndian.Uint32(flat[81:]))
	c.co1[2] = math.Float32frombits(binary.LittleEndian.Uint32(flat[85:]))
	c.calT1[0] = math.Float32frombits(binary.LittleEndian.Uint32(flat[89:]))
	c.calT1[1] = math.Float32frombits(binary.LittleEndian.Uint32(flat[93:]))
	c.calT1[2] = math.Float32frombits(binary.LittleEndian.Uint32(flat[97:]))
	return c, true
}

// TemperatureC считает °C из PTU и накопленной калибровки; ok=false, если cal/PTU не готовы.
func (s *CalStore) TemperatureC(id string, frame []byte) (float64, bool) {
	if !checkTLV(frame, posPTU) {
		return 0, false
	}
	c, ok := s.loadPTU(id)
	if !ok {
		return 0, false
	}
	f := u24(frame[posPTU+2:])
	f1 := u24(frame[posPTU+5:])
	f2 := u24(frame[posPTU+8:])
	if f2 == f1 || c.rf2 == c.rf1 {
		return 0, false
	}
	return getT(f, f1, f2, c), true
}

func u24(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16
}

// getT — платиновый датчик RS41 (rs1729 / radiosonde_auto_rx).
func getT(f, f1, f2 uint32, c ptuCal) float64 {
	g := float64(f2-f1) / float64(c.rf2-c.rf1)
	rb := (float64(f1)*float64(c.rf2) - float64(f2)*float64(c.rf1)) / float64(f2-f1)
	rc := float64(f)/g - rb
	r := rc * float64(c.calT1[0])
	p0, p1, p2 := float64(c.co1[0]), float64(c.co1[1]), float64(c.co1[2])
	t := (p0 + p1*r + p2*r*r + float64(c.calT1[1])) * (1 + float64(c.calT1[2]))
	return t
}
