package modem

import "testing"

func TestPRBS9Checker_LocksMidSequenceWithoutErrors(t *testing.T) {
	bits := prbs9Bits(2000)[137:]
	var c PRBS9Checker
	c.Push(bits)

	if !c.Locked() {
		t.Fatal("checker not locked on a clean sequence")
	}
	if c.Errors() != 0 {
		t.Errorf("errors=%d, want 0", c.Errors())
	}
	if want := len(bits) - 9; c.Bits() != want {
		t.Errorf("bits=%d, want %d (all but the 9 seed bits)", c.Bits(), want)
	}
}

func TestPRBS9Checker_CountsEachFlippedBitOnce(t *testing.T) {
	bits := prbs9Bits(3000)
	flipped := 0
	for i := 100; i < len(bits); i += 100 {
		bits[i] ^= 1
		flipped++
	}
	var c PRBS9Checker
	c.Push(bits)

	if c.Errors() != flipped {
		t.Errorf("errors=%d, want %d: an error must not propagate into the prediction", c.Errors(), flipped)
	}
	if got, want := c.BER(), float64(flipped)/float64(c.Bits()); got != want {
		t.Errorf("BER=%g, want %g", got, want)
	}
}

func TestPRBS9Checker_ResyncsAfterBitSlip(t *testing.T) {
	bits := prbs9Bits(3000)
	slipped := append(append([]byte{}, bits[:1000]...), bits[1001:]...)
	var c PRBS9Checker
	c.Push(slipped)

	if !c.Locked() {
		t.Fatal("checker did not relock after a dropped bit")
	}
	// Until the slip is detected the checker sees ~50% errors within one block.
	if c.Errors() > prbsCheckBlock {
		t.Errorf("errors=%d, want at most one block (%d)", c.Errors(), prbsCheckBlock)
	}
	if c.Errors() == 0 {
		t.Error("errors=0: the slip must be counted")
	}
}

func TestPRBS9Checker_SilenceDoesNotLock(t *testing.T) {
	var c PRBS9Checker
	c.Push(make([]byte, 1000))
	if c.Locked() || c.Bits() != 0 {
		t.Errorf("locked=%v bits=%d on all zeros, want no lock", c.Locked(), c.Bits())
	}
}

func TestPRBS9Checker_StateContinuesAcrossCalls(t *testing.T) {
	bits := prbs9Bits(1500)
	var c PRBS9Checker
	c.Push(bits[:5])
	c.Push(bits[5:700])
	c.Push(bits[700:])
	if !c.Locked() || c.Errors() != 0 || c.Bits() != len(bits)-9 {
		t.Errorf("locked=%v errors=%d bits=%d, want locked, 0, %d", c.Locked(), c.Errors(), c.Bits(), len(bits)-9)
	}
}
