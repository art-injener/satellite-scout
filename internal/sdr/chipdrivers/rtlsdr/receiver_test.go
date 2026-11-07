package rtlsdr

import (
	"context"
	"errors"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

// fakeStream hands out queued bulk buffers until closed.
type fakeStream struct {
	chunks    chan []byte
	errs      chan error
	closed    chan struct{}
	closeOnce sync.Once
}

func newFakeStream() *fakeStream {
	return &fakeStream{chunks: make(chan []byte, 8), errs: make(chan error, 1), closed: make(chan struct{})}
}

var errStreamClosed = errors.New("fake stream closed")

func (s *fakeStream) Read() ([]byte, error) {
	select {
	case b := <-s.chunks:
		return b, nil
	case err := <-s.errs:
		return nil, err
	case <-s.closed:
		return nil, errStreamClosed
	}
}

func (s *fakeStream) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

// fakeDongle is an RTL2832U with an R820T behind it.
type fakeDongle struct {
	*tunerSim
	mu       sync.Mutex
	events   []string
	stream   *fakeStream
	bufSize  int
	isClosed bool
}

func newFakeDongle() *fakeDongle {
	d := &fakeDongle{tunerSim: newTunerSim(), stream: newFakeStream()}
	prev := d.f.fail
	d.f.fail = func(c ctrlCall) error {
		if c.out() && c.value == regEPACtl && c.index == uint16(blockUSB)<<8|0x10 && c.data[0] == 0 {
			d.event("fifo-reset")
		}
		return prev(c)
	}
	return d
}

func (d *fakeDongle) event(e string) {
	d.mu.Lock()
	d.events = append(d.events, e)
	d.mu.Unlock()
}

func (d *fakeDongle) Control(rt, req uint8, v, i uint16, data []byte, to time.Duration) (int, error) {
	return d.f.Control(rt, req, v, i, data, to)
}

func (d *fakeDongle) ClearHalt(ep uint8) error {
	if ep == bulkEndpoint {
		d.event("clear-halt")
	}
	return nil
}

func (d *fakeDongle) OpenStream(ep uint8, buffers, size int) (bulkStream, error) {
	d.mu.Lock()
	d.bufSize = size
	d.mu.Unlock()
	d.event("stream")
	return d.stream, nil
}

func (d *fakeDongle) Close() error {
	d.mu.Lock()
	d.isClosed = true
	d.mu.Unlock()
	d.event("close")
	return nil
}

func (d *fakeDongle) closed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.isClosed
}

func (d *fakeDongle) log() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.events...)
}

func newFakeReceiver(dongles ...*fakeDongle) *Receiver {
	var n int
	return New(Options{open: func(Options) (usbDevice, error) {
		if n >= len(dongles) {
			return nil, errors.New("no dongle")
		}
		n++
		return dongles[n-1], nil
	}})
}

func startStream(t *testing.T, r *Receiver, rate uint32, block int) <-chan sdr.IQFrame {
	t.Helper()
	ch, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: rate, BlockSize: block})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Stop() })
	return ch
}

func nextFrame(t *testing.T, ch <-chan sdr.IQFrame) sdr.IQFrame {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatal("stream closed")
		}
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("no frame")
	}
	return sdr.IQFrame{}
}

func waitClosed(t *testing.T, ch <-chan sdr.IQFrame) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("stream not closed")
		}
	}
}

func TestReceiver_StreamConvertsUnsignedSamples(t *testing.T) {
	d := newFakeDongle()
	r := newFakeReceiver(d)
	ch := startStream(t, r, 2_048_000, 256)

	if d.bufSize%512 != 0 || d.bufSize < 2*256 {
		t.Errorf("bulk buffer %d bytes: want a multiple of 512 holding the block", d.bufSize)
	}
	d.stream.chunks <- []byte{0, 255, 127, 128}
	f := nextFrame(t, ch)
	want := []complex64{complex(-1, 1), complex(-0.5/127.5, 0.5/127.5)}
	if len(f.Samples) != len(want) {
		t.Fatalf("%d samples, want %d", len(f.Samples), len(want))
	}
	for i := range want {
		if f.Samples[i] != want[i] {
			t.Errorf("sample %d = %v, want %v", i, f.Samples[i], want[i])
		}
	}
	if f.SampleRate != 2_048_000 || f.CenterHz != DefaultFrequencyHz {
		t.Errorf("frame at %d Hz, %d S/s", f.CenterHz, f.SampleRate)
	}
}

func TestReceiver_BringUpThenFIFOResetBeforeStream(t *testing.T) {
	d := newFakeDongle()
	r := newFakeReceiver(d)
	startStream(t, r, 2_048_000, 0)

	got := strings.Join(d.log(), ",")
	if !strings.HasSuffix(got, "fifo-reset,clear-halt,stream") {
		t.Errorf("events %q: FIFO reset and host-side unstall must precede the stream", got)
	}
	for i, v := range r820tInitRegs[:3] {
		if d.regs[5+i]&0x0f == 0 && v&0x0f != 0 {
			t.Errorf("tuner not initialised: reg 0x%02x = 0x%02x", 5+i, d.regs[5+i])
		}
	}
	// 0x44 is the nint word for 436.5 MHz at the 6 MHz IF.
	if d.regs[0x14] != 0x44 {
		t.Errorf("tuner not at 436.5 MHz: reg 0x14 = 0x%02x", d.regs[0x14])
	}
	ifHz := map[uint16]byte{}
	for _, c := range d.f.writes() {
		if isDemodWrite(c, 1, 0x19) || isDemodWrite(c, 1, 0x1a) || isDemodWrite(c, 1, 0x1b) {
			ifHz[c.value>>8] = c.data[0]
		}
	}
	if ifHz[0x19] != 0x32 || ifHz[0x1a] != 0xaa || ifHz[0x1b] != 0xab {
		t.Errorf("demod IF = %x, want 6 MHz", ifHz)
	}
	if st := r.Status(); !st.Running || st.SampleRate != 2_048_000 || st.GainDB >= 0 {
		t.Errorf("status %+v: want running at 2.048 MS/s with AGC", st)
	}
}

func TestReceiver_TuneIsKeptUntilStartAndAppliedLive(t *testing.T) {
	d := newFakeDongle()
	r := newFakeReceiver(d)
	if err := r.Tune(145_800_000); err != nil {
		t.Fatal(err)
	}
	ch := startStream(t, r, 2_048_000, 0)
	at145 := d.regs[0x14]

	d.stream.chunks <- []byte{1, 2}
	if f := nextFrame(t, ch); f.CenterHz != 145_800_000 {
		t.Errorf("frame at %d Hz, want 145.8 MHz", f.CenterHz)
	}
	if err := r.Tune(436_500_000); err != nil {
		t.Fatal(err)
	}
	if d.regs[0x14] == at145 || d.regs[0x14] != 0x44 {
		t.Errorf("live tune did not reach the tuner: reg 0x14 = 0x%02x", d.regs[0x14])
	}
	d.stream.chunks <- []byte{1, 2}
	if f := nextFrame(t, ch); f.CenterHz != 436_500_000 {
		t.Errorf("frame at %d Hz after retune", f.CenterHz)
	}
	if err := r.Tune(10_000_000); err == nil {
		t.Error("10 MHz accepted")
	}
}

func TestReceiver_PPMFromOptionsReachesChip(t *testing.T) {
	d := newFakeDongle()
	r := New(Options{PPM: 50, open: func(Options) (usbDevice, error) { return d, nil }})
	startStream(t, r, 2_048_000, 0)

	var corr []ctrlCall
	for _, w := range d.f.writes() {
		if isDemodWrite(w, 1, 0x3f) {
			corr = append(corr, w)
		}
	}
	if len(corr) == 0 || corr[len(corr)-1].data[0] != 0xba {
		t.Errorf("sample clock correction %+v, want 0xba for +50 ppm", corr)
	}
}

func TestReceiver_GainAppliedLive(t *testing.T) {
	d := newFakeDongle()
	r := newFakeReceiver(d)
	startStream(t, r, 2_048_000, 0)

	if err := r.SetGain(30); err != nil {
		t.Fatal(err)
	}
	if d.regs[0x05]&0x10 == 0 || d.regs[0x05]&0x0f != 9 {
		t.Errorf("manual 30 dB: reg 0x05 = 0x%02x", d.regs[0x05])
	}
	if r.Status().GainDB != 30 {
		t.Errorf("status gain %v", r.Status().GainDB)
	}
	if err := r.SetGain(-1); err != nil {
		t.Fatal(err)
	}
	if d.regs[0x05]&0x10 != 0 {
		t.Error("AGC not restored")
	}
}

func TestReceiver_StopPowersDownAndReleases(t *testing.T) {
	d1, d2 := newFakeDongle(), newFakeDongle()
	r := newFakeReceiver(d1, d2)
	ch := startStream(t, r, 2_048_000, 0)

	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, ch)
	if !d1.closed() {
		t.Error("device not closed")
	}
	if d1.regs[0x06] != 0xb1 {
		t.Error("tuner not in standby")
	}
	if r.Status().Running {
		t.Error("still running")
	}
	if err := r.Stop(); err != nil {
		t.Errorf("second stop: %v", err)
	}
	startStream(t, r, 1_024_000, 0)
	if r.Status().SampleRate != 1_024_000 {
		t.Errorf("restart at %d S/s", r.Status().SampleRate)
	}
}

func TestReceiver_DeviceLossEndsStream(t *testing.T) {
	d := newFakeDongle()
	r := newFakeReceiver(d)
	ch := startStream(t, r, 2_048_000, 0)

	d.stream.errs <- syscall.ENODEV
	waitClosed(t, ch)
	if !d.closed() {
		t.Error("device not closed")
	}
	if r.Status().Running {
		t.Error("still running")
	}
}

func TestReceiver_StartFailuresCloseDevice(t *testing.T) {
	noTuner := newFakeDongle()
	noTuner.regs[0] = 0x00
	r := newFakeReceiver(noTuner)
	if _, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: 2_048_000}); err == nil ||
		!strings.Contains(err.Error(), "tuner") {
		t.Errorf("no tuner: err = %v", err)
	}
	if !noTuner.closed() {
		t.Error("device left open without a tuner")
	}

	gap := newFakeDongle()
	r = newFakeReceiver(gap)
	if _, err := r.StartStream(context.Background(), sdr.StreamConfig{SampleRate: 600_000}); err == nil {
		t.Error("600 kS/s accepted")
	}
	if !gap.closed() {
		t.Error("device left open after a bad sample rate")
	}
	if r.Status().Running {
		t.Error("running after a failed start")
	}
}

func TestProbe_ReadsSamplesAndReleases(t *testing.T) {
	d := newFakeDongle()
	d.stream.chunks <- make([]byte, 2*probeSamples)
	rep, err := probe(newFakeReceiver(d))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tuner != "R820T/R820T2" || rep.Samples < probeSamples || rep.SampleRate != RecommendedSampleRate ||
		rep.CenterHz != DefaultFrequencyHz || rep.GainDB >= 0 {
		t.Errorf("report %+v", rep)
	}
	if !d.closed() {
		t.Error("probe left the dongle open")
	}

	if _, err := probe(newFakeReceiver()); err == nil {
		t.Error("probe without a dongle succeeded")
	}
}

func TestReceiver_CapabilitiesSkipResamplerGap(t *testing.T) {
	caps := New(Options{}).Capabilities()
	if got := caps.UsableBandHz(caps.RecommendedSampleRate); got != 2_048_000 {
		t.Errorf("usable band %d at the recommended rate, want 2048000", got)
	}
	choices := sdr.SampleRateChoices(caps, caps.RecommendedSampleRate)
	want := []uint32{2_560_000, 1_280_000, 256_000} // 640 kHz in the gap, 320 kHz above the low range
	if len(choices) != len(want) {
		t.Fatalf("choices %v, want %v", choices, want)
	}
	for i := range want {
		if choices[i] != want[i] {
			t.Fatalf("choices %v, want %v", choices, want)
		}
	}
}
