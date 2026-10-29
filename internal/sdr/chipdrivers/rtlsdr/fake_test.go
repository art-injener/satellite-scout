package rtlsdr

import (
	"sync"
	"time"
)

// ctrlCall is one vendor control transfer as it went over the wire.
type ctrlCall struct {
	reqType uint8
	value   uint16
	index   uint16
	data    []byte // payload of an OUT transfer, nil for IN
	inLen   int    // requested length of an IN transfer
}

func (c ctrlCall) out() bool { return c.reqType == reqOut }

// fakeCtrl records control transfers and answers IN transfers via respond.
type fakeCtrl struct {
	mu      sync.Mutex
	calls   []ctrlCall
	respond func(c ctrlCall) []byte
	fail    func(c ctrlCall) error
}

func (f *fakeCtrl) Control(reqType, _ uint8, value, index uint16, data []byte, _ time.Duration) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := ctrlCall{reqType: reqType, value: value, index: index}
	if reqType == reqOut {
		c.data = append([]byte(nil), data...)
	} else {
		c.inLen = len(data)
	}
	f.calls = append(f.calls, c)
	if f.fail != nil {
		if err := f.fail(c); err != nil {
			return 0, err
		}
	}
	if reqType == reqOut {
		return len(data), nil
	}
	// Like the chip, an IN transfer always returns the requested length; unanswered bytes are zero.
	clear(data)
	if f.respond != nil {
		copy(data, f.respond(c))
	}
	return len(data), nil
}

func (f *fakeCtrl) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func (f *fakeCtrl) snapshot() []ctrlCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ctrlCall(nil), f.calls...)
}

// writes returns OUT transfers only, dropping the commit reads that follow demod writes.
func (f *fakeCtrl) writes() []ctrlCall {
	var out []ctrlCall
	for _, c := range f.snapshot() {
		if c.out() {
			out = append(out, c)
		}
	}
	return out
}

// demodWrite is the expected OUT transfer of a demod register write.
func demodWrite(page uint8, addr uint16, data ...byte) ctrlCall {
	return ctrlCall{reqType: reqOut, value: addr<<8 | 0x20, index: 0x10 | uint16(page), data: data}
}

// blockWrite is the expected OUT transfer of a block register write.
func blockWrite(block uint8, addr uint16, data ...byte) ctrlCall {
	return ctrlCall{reqType: reqOut, value: addr, index: uint16(block)<<8 | 0x10, data: data}
}

// isDemodWrite reports whether c writes the given demod register.
func isDemodWrite(c ctrlCall, page uint8, addr uint16) bool {
	return c.out() && c.value == addr<<8|0x20 && c.index == 0x10|uint16(page)
}
