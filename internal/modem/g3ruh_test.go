package modem

import (
	"bytes"
	"math/rand/v2"
	"testing"
)

func randomBits(n int, seed uint64) []byte {
	r := rand.New(rand.NewPCG(seed, 0))
	bits := make([]byte, n)
	for i := range bits {
		bits[i] = byte(r.IntN(2))
	}
	return bits
}

// scrambleInBlocks scrambles bits fed in blocks of the given size.
func scrambleInBlocks(bits []byte, block int) []byte {
	var s G3RUHScrambler
	var out []byte
	for i := 0; i < len(bits); i += block {
		out = s.Scramble(out, bits[i:min(i+block, len(bits))])
	}
	return out
}

func TestG3RUH_RoundTrip(t *testing.T) {
	bits := randomBits(5000, 1)
	line := scrambleInBlocks(bits, 7)
	if bytes.Equal(line, bits) {
		t.Fatal("scrambler left the bits unchanged")
	}
	if !bytes.Equal(line, scrambleInBlocks(bits, len(bits))) {
		t.Fatal("block split changes the scrambler output")
	}

	var d G3RUHDescrambler
	var got []byte
	for i := 0; i < len(line); i += 13 {
		got = d.Descramble(got, line[i:min(i+13, len(line))])
	}
	if !bytes.Equal(got, bits) {
		t.Fatal("descrambled bits differ from the original")
	}
}

// The descrambler depends only on the last 17 received bits, so it locks to a stream
// joined at any point.
func TestG3RUH_DescramblerSelfSynchronizes(t *testing.T) {
	bits := randomBits(1000, 2)
	line := scrambleInBlocks(bits, len(bits))

	var d G3RUHDescrambler
	d.Descramble(nil, randomBits(40, 3)) // unrelated history
	got := d.Descramble(nil, line[300:])
	if !bytes.Equal(got[17:], bits[317:]) {
		t.Fatal("descrambler did not lock after 17 bits")
	}
}

// One line error leaves the descrambler at three places: the bit itself and 12 and 17 bits later.
func TestG3RUH_SingleLineErrorGivesThreeErrors(t *testing.T) {
	bits := randomBits(200, 4)
	line := scrambleInBlocks(bits, len(bits))
	line[50] ^= 1

	var d G3RUHDescrambler
	got := d.Descramble(nil, line)
	var errs []int
	for i := range got {
		if got[i] != bits[i] {
			errs = append(errs, i)
		}
	}
	if len(errs) != 3 || errs[0] != 50 || errs[1] != 62 || errs[2] != 67 {
		t.Errorf("error positions=%v, want [50 62 67]", errs)
	}
}

// 1 + x^12 + x^17 is primitive and 2^17 − 1 is prime: the impulse response repeats
// with period exactly 131071.
func TestG3RUH_ImpulseResponsePeriod(t *testing.T) {
	const period = 1<<17 - 1
	in := make([]byte, 2*period+100)
	in[0] = 1
	var s G3RUHScrambler
	out := s.Scramble(nil, in)
	for i := 1; i < period+100; i++ {
		if out[i] != out[i+period] {
			t.Fatalf("bit %d differs from bit %d", i, i+period)
		}
	}
	ones := 0
	for _, b := range out[1 : period+1] {
		ones += int(b)
	}
	// An m-sequence has 2^16 ones per period.
	if ones != 1<<16 {
		t.Errorf("ones per period=%d, want %d", ones, 1<<16)
	}
}
