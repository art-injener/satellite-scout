package rtlsdr

import (
	"reflect"
	"testing"
)

func TestChip_DemodWriteIsCommittedByRead(t *testing.T) {
	f := &fakeCtrl{}
	c := newChip(f)
	if err := c.writeDemod(1, 0x15, 0x01, 1); err != nil {
		t.Fatal(err)
	}
	want := []ctrlCall{
		demodWrite(1, 0x15, 0x01),
		{reqType: reqIn, value: 0x01<<8 | 0x20, index: 0x0a, inLen: 1},
	}
	if got := f.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
}

func TestChip_TwoByteWritesAreBigEndian(t *testing.T) {
	f := &fakeCtrl{}
	c := newChip(f)
	if err := c.writeReg(blockUSB, regEPACtl, 0x1002, 2); err != nil {
		t.Fatal(err)
	}
	want := []ctrlCall{blockWrite(blockUSB, regEPACtl, 0x10, 0x02)}
	if got := f.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
}

func TestChip_SampleRateProgramsResampler(t *testing.T) {
	cases := []struct {
		rate       uint32
		high, low  uint16
		actualRate uint32
	}{
		{2_048_000, 0x0384, 0x0000, 2_048_000},
		{2_400_000, 0x0300, 0x0000, 2_400_000},
		// Below the gap the 28-bit register drops bit 28; the chip restores it from bit 27.
		{256_000, 0x0c20, 0x0000, 256_000},
	}
	for _, tc := range cases {
		f := &fakeCtrl{}
		c := newChip(f)
		got, err := c.setSampleRate(tc.rate)
		if err != nil {
			t.Fatalf("%d: %v", tc.rate, err)
		}
		if got != tc.actualRate {
			t.Errorf("%d: actual rate %d, want %d", tc.rate, got, tc.actualRate)
		}
		w := f.writes()
		if len(w) < 2 ||
			!reflect.DeepEqual(w[0], demodWrite(1, 0x9f, byte(tc.high>>8), byte(tc.high))) ||
			!reflect.DeepEqual(w[1], demodWrite(1, 0xa1, byte(tc.low>>8), byte(tc.low))) {
			t.Errorf("%d: resampler writes %+v", tc.rate, w)
		}
		last := w[len(w)-2:]
		if !reflect.DeepEqual(last, []ctrlCall{demodWrite(1, 0x01, 0x14), demodWrite(1, 0x01, 0x10)}) {
			t.Errorf("%d: no soft reset after the divider: %+v", tc.rate, last)
		}
	}
}

func TestChip_SampleRateWritesPPMCorrection(t *testing.T) {
	cases := []struct {
		ppm     int
		hi, low byte
	}{
		{0, 0x00, 0x00},
		// −50·2^24/10^6 = −838 = 0xfcba: 6 high bits to 0x3e, low byte to 0x3f.
		{50, 0x3c, 0xba},
		{-50, 0x03, 0x46},
	}
	for _, tc := range cases {
		f := &fakeCtrl{}
		c := newChip(f)
		c.ppm = tc.ppm
		if _, err := c.setSampleRate(2_048_000); err != nil {
			t.Fatal(err)
		}
		var got []ctrlCall
		for _, w := range f.writes() {
			if isDemodWrite(w, 1, 0x3f) || isDemodWrite(w, 1, 0x3e) {
				got = append(got, w)
			}
		}
		want := []ctrlCall{demodWrite(1, 0x3f, tc.low), demodWrite(1, 0x3e, tc.hi)}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ppm %d: correction writes %+v, want %+v", tc.ppm, got, want)
		}
	}
}

func TestChip_IFFrequencyUsesCorrectedXtal(t *testing.T) {
	f := &fakeCtrl{}
	c := newChip(f)
	c.ppm = 100
	if err := c.setIFFreq(3_570_000); err != nil {
		t.Fatal(err)
	}
	// 3.57 MHz·2^22 / 28.80288 MHz = 519866, negated.
	want := []ctrlCall{demodWrite(1, 0x19, 0x38), demodWrite(1, 0x1a, 0x11), demodWrite(1, 0x1b, 0x46)}
	if got := f.writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes = %+v, want %+v", got, want)
	}
}

func TestChip_SampleRateRejectsGapAndRange(t *testing.T) {
	for _, rate := range []uint32{225_000, 300_001, 600_000, 900_000, 3_200_001} {
		f := &fakeCtrl{}
		if _, err := newChip(f).setSampleRate(rate); err == nil {
			t.Errorf("%d: accepted", rate)
		}
		if len(f.snapshot()) != 0 {
			t.Errorf("%d: touched the chip", rate)
		}
	}
	for _, rate := range []uint32{225_001, 300_000, 900_001, 3_200_000} {
		if _, err := newChip(&fakeCtrl{}).setSampleRate(rate); err != nil {
			t.Errorf("%d: %v", rate, err)
		}
	}
}

func TestChip_IFFrequencyIsNegatedFixedPoint(t *testing.T) {
	f := &fakeCtrl{}
	if err := newChip(f).setIFFreq(3_570_000); err != nil {
		t.Fatal(err)
	}
	want := []ctrlCall{demodWrite(1, 0x19, 0x38), demodWrite(1, 0x1a, 0x11), demodWrite(1, 0x1b, 0x12)}
	if got := f.writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes = %+v, want %+v", got, want)
	}
}

func TestChip_FIRPacksTwelveBitTaps(t *testing.T) {
	f := &fakeCtrl{}
	if err := newChip(f).setFIR(firDefault); err != nil {
		t.Fatal(err)
	}
	w := f.writes()
	if len(w) != 20 {
		t.Fatalf("%d FIR writes, want 20", len(w))
	}
	var raw [20]byte
	for i, c := range w {
		if !isDemodWrite(c, 1, 0x1c+uint16(i)) {
			t.Fatalf("write %d goes to %+v", i, c)
		}
		raw[i] = c.data[0]
	}
	for i := 0; i < 8; i++ {
		if int(int8(raw[i])) != firDefault[i] {
			t.Errorf("tap %d = %d, want %d", i, int8(raw[i]), firDefault[i])
		}
	}
	for i := 0; i < 4; i++ {
		b := raw[8+3*i:]
		v0 := sign12(int(b[0])<<4 | int(b[1])>>4)
		v1 := sign12(int(b[1]&0x0f)<<8 | int(b[2]))
		if v0 != firDefault[8+2*i] || v1 != firDefault[9+2*i] {
			t.Errorf("taps %d,%d = %d,%d, want %d,%d", 8+2*i, 9+2*i, v0, v1, firDefault[8+2*i], firDefault[9+2*i])
		}
	}
}

func sign12(v int) int {
	if v >= 0x800 {
		return v - 0x1000
	}
	return v
}

func TestChip_InitBasebandOrder(t *testing.T) {
	f := &fakeCtrl{}
	if err := newChip(f).initBaseband(); err != nil {
		t.Fatal(err)
	}
	w := f.writes()
	if !reflect.DeepEqual(w[0], blockWrite(blockUSB, regSysctl, 0x09)) {
		t.Errorf("first write %+v, want USB_SYSCTL 0x09", w[0])
	}
	if !reflect.DeepEqual(w[len(w)-1], demodWrite(0, 0x0d, 0x83)) {
		t.Errorf("last write %+v, want TP_CK0 clock off", w[len(w)-1])
	}
	firAt, sdrAt := -1, -1
	for i, c := range w {
		if firAt < 0 && isDemodWrite(c, 1, 0x1c) {
			firAt = i
		}
		if isDemodWrite(c, 0, 0x19) {
			sdrAt = i
		}
	}
	if firAt < 0 || sdrAt < firAt+20 {
		t.Errorf("FIR at %d, SDR mode at %d: FIR must be uploaded before SDR mode", firAt, sdrAt)
	}
}

func TestChip_I2CGoesThroughBridgeBlock(t *testing.T) {
	f := &fakeCtrl{respond: func(ctrlCall) []byte { return []byte{0x69} }}
	c := newChip(f)
	got, err := c.i2cReadReg(0x34, 0x00)
	if err != nil {
		t.Fatal(err)
	}
	if got != 0x69 {
		t.Errorf("read 0x%02x, want 0x69", got)
	}
	want := []ctrlCall{
		{reqType: reqOut, value: 0x34, index: 0x0610, data: []byte{0x00}},
		{reqType: reqIn, value: 0x34, index: 0x0600, inLen: 1},
	}
	if got := f.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %+v, want %+v", got, want)
	}
}

func TestChip_RepeaterTogglesOnlyOnChange(t *testing.T) {
	f := &fakeCtrl{}
	c := newChip(f)
	for _, on := range []bool{true, true, false, false} {
		if err := c.setRepeater(on); err != nil {
			t.Fatal(err)
		}
	}
	want := []ctrlCall{demodWrite(1, 0x01, 0x18), demodWrite(1, 0x01, 0x10)}
	if got := f.writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes = %+v, want %+v", got, want)
	}
}

func TestChip_ResetBufferPulsesFIFOReset(t *testing.T) {
	f := &fakeCtrl{}
	if err := newChip(f).resetBuffer(); err != nil {
		t.Fatal(err)
	}
	want := []ctrlCall{blockWrite(blockUSB, regEPACtl, 0x10, 0x02), blockWrite(blockUSB, regEPACtl, 0x00, 0x00)}
	if got := f.writes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("writes = %+v, want %+v", got, want)
	}
}
