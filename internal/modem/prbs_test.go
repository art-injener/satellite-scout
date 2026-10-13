package modem

import "testing"

func prbs9Bits(n int) []byte {
	p := NewPRBS9()
	bits := make([]byte, n)
	for i := range bits {
		bits[i] = p.Next()
	}
	return bits
}

func TestPRBS9_StartsWithSeedOfOnes(t *testing.T) {
	for i, b := range prbs9Bits(10) {
		want := byte(1)
		if i == 9 {
			want = 0 // feedback of the all-ones state: 1 xor 1
		}
		if b != want {
			t.Fatalf("bit %d = %d, want %d", i, b, want)
		}
	}
}

func TestPRBS9_Period511(t *testing.T) {
	bits := prbs9Bits(3 * PRBS9Period)
	for i := range 2 * PRBS9Period {
		if bits[i] != bits[i+PRBS9Period] {
			t.Fatalf("bit %d differs from bit %d", i, i+PRBS9Period)
		}
	}
	// 511 = 7·73: the sequence must not repeat with any proper divisor
	for _, p := range []int{7, 73} {
		periodic := true
		for i := range PRBS9Period {
			if bits[i] != bits[i+p] {
				periodic = false
				break
			}
		}
		if periodic {
			t.Errorf("sequence repeats with period %d", p)
		}
	}
}

func TestPRBS9_BalanceAndSingleRunOfNineOnes(t *testing.T) {
	bits := prbs9Bits(PRBS9Period + 8)
	ones := 0
	for _, b := range bits[:PRBS9Period] {
		ones += int(b)
	}
	if ones != 256 {
		t.Errorf("ones per period = %d, want 256", ones)
	}

	runs := 0
	for i := range PRBS9Period {
		all := true
		for _, b := range bits[i : i+9] {
			if b == 0 {
				all = false
				break
			}
		}
		if all {
			runs++
		}
	}
	if runs != 1 {
		t.Errorf("windows of nine ones per period = %d, want 1", runs)
	}
}
