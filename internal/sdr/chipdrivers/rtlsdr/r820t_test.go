package rtlsdr

import (
	"math"
	"syscall"
	"testing"
)

// tunerSim is an R820T behind the I2C bridge: writes land in its registers, reads come back
// from the register pointer bit-reversed, the way the chip sends them.
type tunerSim struct {
	f    *fakeCtrl
	regs [32]byte // logical values; status registers 0..4 are preset
	ptr  int
	// burst is the largest I2C write payload seen, register pointer included.
	burst int
}

func newTunerSim() *tunerSim {
	s := &tunerSim{f: &fakeCtrl{}}
	s.regs[0] = 0x96 // chip ID, 0x69 on the wire
	s.regs[2] = 0x40 // PLL locked
	s.regs[4] = 0x20 // VCO fine tune = 2, no divider correction
	s.f.respond = s.respond
	prev := s.f.fail
	s.f.fail = func(c ctrlCall) error {
		if c.out() && c.index == 0x0610 && c.value == uint16(r820tAddr) {
			s.write(c.data)
		}
		if prev != nil {
			return prev(c)
		}
		return nil
	}
	return s
}

func (s *tunerSim) write(data []byte) {
	s.burst = max(s.burst, len(data))
	s.ptr = int(data[0])
	for i, b := range data[1:] {
		s.regs[s.ptr+i] = b
	}
}

func (s *tunerSim) respond(c ctrlCall) []byte {
	if c.index != 0x0600 || c.value != uint16(r820tAddr) {
		return nil
	}
	out := make([]byte, c.inLen)
	for i := range out {
		out[i] = bitReverse(s.regs[s.ptr+i])
	}
	return out
}

func newSimTuner(t *testing.T) (*tunerSim, *r820t) {
	t.Helper()
	s := newTunerSim()
	tu := newR820T(newChip(s.f))
	if err := tu.init(); err != nil {
		t.Fatal(err)
	}
	return s, tu
}

func TestR820T_DetectByChipID(t *testing.T) {
	s := newTunerSim()
	if !detectR820T(newChip(s.f)) {
		t.Fatal("R820T not detected")
	}

	absent := &fakeCtrl{fail: func(c ctrlCall) error {
		if c.index>>8 == uint16(blockI2C) {
			return syscall.EPIPE
		}
		return nil
	}}
	if detectR820T(newChip(absent)) {
		t.Fatal("detected a tuner that does not answer")
	}
	if w := absent.writes(); !isDemodWrite(w[len(w)-1], 1, 0x01) || w[len(w)-1].data[0] != 0x10 {
		t.Fatalf("I2C repeater left open: %+v", w[len(w)-1])
	}
}

func TestR820T_InitLoadsRegistersInShortBursts(t *testing.T) {
	s, _ := newSimTuner(t)
	for i, v := range r820tInitRegs {
		if s.regs[5+i] != v {
			t.Errorf("reg 0x%02x = 0x%02x, want 0x%02x", 5+i, s.regs[5+i], v)
		}
	}
	if s.burst > r820tMaxBurst+1 {
		t.Errorf("I2C burst of %d bytes, the bridge takes %d", s.burst, r820tMaxBurst+1)
	}
	w := s.f.writes()
	if !isDemodWrite(w[0], 1, 0x01) || w[0].data[0] != 0x18 {
		t.Errorf("first write %+v, want I2C repeater on", w[0])
	}
	if last := w[len(w)-1]; !isDemodWrite(last, 1, 0x01) || last.data[0] != 0x10 {
		t.Errorf("last write %+v, want I2C repeater off", last)
	}
}

func TestR820T_TuneProgramsPLL(t *testing.T) {
	s, tu := newSimTuner(t)
	if err := tu.setFreq(436_500_000); err != nil {
		t.Fatal(err)
	}
	// LO 440.07 MHz, mixer divider 8: VCO 3520.56 MHz = 2·28.8 MHz·(61 + 7918/65536).
	checks := []struct {
		reg       int
		mask, val byte
		what      string
	}{
		{0x14, 0xff, 0x0c, "nint 61"},
		{0x16, 0xff, 0x1e, "sdm high"},
		{0x15, 0xff, 0xee, "sdm low"},
		{0x10, 0xe0, 2 << 5, "mixer divider 8"},
		{0x10, 0x1b, 0x00, "no refdiv2, xtal cap 0p"},
		{0x12, 0x08, 0x00, "sigma-delta on"},
		{0x12, 0xe0, 0x80, "VCO current 100"},
		{0x1a, 0xc3, 0x41, "RF mux for 310-450 MHz"},
		{0x1a, 0x08, 0x08, "PLL autotune 8 kHz after lock"},
		{0x1b, 0xff, 0x00, "tracking filter"},
		{0x17, 0x08, 0x00, "open drain off"},
	}
	for _, c := range checks {
		if got := s.regs[c.reg] & c.mask; got != c.val {
			t.Errorf("%s: reg 0x%02x & 0x%02x = 0x%02x, want 0x%02x", c.what, c.reg, c.mask, got, c.val)
		}
	}
}

func TestR820T_TunedLOMatchesRequest(t *testing.T) {
	for _, f := range []uint32{r820tMinHz, 137_100_000, 145_800_000, 436_500_000, 1_000_000_000, r820tMaxHz} {
		s, tu := newSimTuner(t)
		if err := tu.setFreq(f); err != nil {
			t.Fatalf("%d: %v", f, err)
		}
		lo := float64(f + r820tIFHz)
		mixDiv := 2.0
		for lo*mixDiv < r820tVCOMin {
			mixDiv *= 2
		}
		v := s.regs[0x14]
		nint := 13 + 4*float64(v&0x3f) + float64(v>>6)
		sdm := float64(uint16(s.regs[0x16])<<8 | uint16(s.regs[0x15]))
		got := 2 * xtalHz * (nint + sdm/65536) / mixDiv
		if math.Abs(got-lo) > 1000 {
			t.Errorf("%d Hz: LO %.0f Hz, want %.0f Hz", f, got, lo)
		}
	}
}

func TestR820T_TuneOutsideRangeFails(t *testing.T) {
	_, tu := newSimTuner(t)
	for _, f := range []uint32{r820tMinHz - 1, r820tMaxHz + 1} {
		if err := tu.setFreq(f); err == nil {
			t.Errorf("%d Hz accepted", f)
		}
	}
}

func TestR820T_UnlockedPLLRaisesVCOCurrent(t *testing.T) {
	s, tu := newSimTuner(t)
	s.regs[2] = 0x00
	if err := tu.setFreq(436_500_000); err != nil {
		t.Fatal(err)
	}
	if got := s.regs[0x12] & 0xe0; got != 0x60 {
		t.Errorf("VCO current bits 0x%02x, want 0x60", got)
	}
	if s.regs[0x1a]&0x08 != 0 {
		t.Error("autotune narrowed without a lock")
	}
}

func TestR820T_ManualGainWalksLNAAndMixer(t *testing.T) {
	s, tu := newSimTuner(t)
	// 9+5+13+10+40+10+38+19+13+9+31+10+22+25+26+17 = 297 < 30 dB, the next LNA step reaches 32.8 dB.
	if err := tu.setGain(true, 300); err != nil {
		t.Fatal(err)
	}
	if got := s.regs[0x05] & 0x1f; got != 0x10|9 {
		t.Errorf("reg 0x05 = 0x%02x, want manual LNA index 9", got)
	}
	if got := s.regs[0x07] & 0x1f; got != 8 {
		t.Errorf("reg 0x07 = 0x%02x, want manual mixer index 8", got)
	}
	if got := s.regs[0x0c] & 0x9f; got != 0x08 {
		t.Errorf("VGA 0x%02x, want fixed 16.3 dB (0x08)", got)
	}

	if err := tu.setGain(true, 0); err != nil {
		t.Fatal(err)
	}
	if s.regs[0x05]&0x0f != 0 || s.regs[0x07]&0x0f != 0 {
		t.Errorf("0 dB left LNA %d, mixer %d", s.regs[0x05]&0x0f, s.regs[0x07]&0x0f)
	}
}

func TestR820T_AutoGain(t *testing.T) {
	s, tu := newSimTuner(t)
	if err := tu.setGain(true, 300); err != nil {
		t.Fatal(err)
	}
	if err := tu.setGain(false, 0); err != nil {
		t.Fatal(err)
	}
	if s.regs[0x05]&0x10 != 0 {
		t.Error("LNA AGC off")
	}
	if s.regs[0x07]&0x10 == 0 {
		t.Error("mixer AGC off")
	}
	if got := s.regs[0x0c] & 0x9f; got != 0x0b {
		t.Errorf("VGA 0x%02x, want fixed 26.5 dB (0x0b)", got)
	}
}

func TestR820T_UnchangedRegisterIsNotRewritten(t *testing.T) {
	s, tu := newSimTuner(t)
	if err := tu.setFreq(436_500_000); err != nil {
		t.Fatal(err)
	}
	s.f.reset()
	if err := tu.setGain(false, 0); err != nil {
		t.Fatal(err)
	}
	// Init left LNA and mixer in AGC; only the VGA changes.
	var tunerWrites int
	for _, c := range s.f.writes() {
		if c.index == 0x0610 {
			tunerWrites++
		}
	}
	if tunerWrites != 1 {
		t.Errorf("%d tuner writes, want 1 (VGA only)", tunerWrites)
	}
}

func TestR820T_StandbyPowersDown(t *testing.T) {
	s, tu := newSimTuner(t)
	if err := tu.standby(); err != nil {
		t.Fatal(err)
	}
	if s.regs[0x06] != 0xb1 || s.regs[0x05] != 0xa0 || s.regs[0x19] != 0x0c {
		t.Errorf("standby regs 0x06=0x%02x 0x05=0x%02x 0x19=0x%02x", s.regs[0x06], s.regs[0x05], s.regs[0x19])
	}
}
