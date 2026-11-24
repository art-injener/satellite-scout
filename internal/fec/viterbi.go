package fec

// CCSDS свёртка k=7, r=1/2: полиномы 0x4F и 0x6D (второй инвертирован),
// как у GNU Radio fec.cc_decoder ([79, -109]) и USP.

const (
	vConstraint = 7
	vStates     = 1 << (vConstraint - 1) // 64
	vPolyA      = 0x4f                   // 79
	vPolyB      = 0x6d                   // 109
)

func parityBits(x int) int {
	x ^= x >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x & 1
}

// EncodeViterbiCCSDS кодирует байты в биты линии (два бита на входной бит), MSB вперёд.
// В конец — хвост из (k−1) нулевых бит.
func EncodeViterbiCCSDS(data []byte) []byte {
	return encodeViterbi(data, true)
}

// EncodeViterbiCCSDSTruncated — без хвостовых бит (CC_TRUNCATED, как USP).
func EncodeViterbiCCSDSTruncated(data []byte) []byte {
	return encodeViterbi(data, false)
}

func encodeViterbi(data []byte, flush bool) []byte {
	state := 0
	n := len(data) * 16
	if flush {
		n += (vConstraint - 1) * 2
	}
	out := make([]byte, 0, n)
	encodeBit := func(bit int) {
		a := parityBits(((state << 1) | bit) & vPolyA)
		b := (parityBits(((state << 1) | bit) & vPolyB) & 1) ^ 1
		out = append(out, byte(a&1), byte(b&1))
		state = ((state << 1) | bit) & (vStates - 1)
	}
	for _, by := range data {
		for i := 7; i >= 0; i-- {
			encodeBit(int(by>>uint(i)) & 1)
		}
	}
	if flush {
		for i := 0; i < vConstraint-1; i++ {
			encodeBit(0)
		}
	}
	return out
}

// DecodeViterbiCCSDS — жёсткое декодирование с хвостом (конец в состоянии 0).
func DecodeViterbiCCSDS(bits []byte) []byte {
	return decodeViterbi(bits, true)
}

// DecodeViterbiCCSDSTruncated — без хвоста: лучшее конечное состояние (USP).
func DecodeViterbiCCSDSTruncated(bits []byte) []byte {
	return decodeViterbi(bits, false)
}

func decodeViterbi(bits []byte, flushed bool) []byte {
	nSym := len(bits) / 2
	if nSym < vConstraint {
		return nil
	}
	const inf = 1 << 28
	oldM := make([]int, vStates)
	newM := make([]int, vStates)
	for i := range oldM {
		oldM[i] = inf
	}
	oldM[0] = 0
	prev := make([][]uint8, nSym)
	inBit := make([][]byte, nSym)
	for t := 0; t < nSym; t++ {
		prev[t] = make([]uint8, vStates)
		inBit[t] = make([]byte, vStates)
		for i := range newM {
			newM[i] = inf
		}
		sym0, sym1 := int(bits[2*t]&1), int(bits[2*t+1]&1)
		for state := 0; state < vStates; state++ {
			m := oldM[state]
			if m >= inf {
				continue
			}
			for bit := 0; bit < 2; bit++ {
				ns := ((state << 1) | bit) & (vStates - 1)
				exp0 := parityBits(((state << 1) | bit) & vPolyA) & 1
				exp1 := (parityBits(((state << 1) | bit) & vPolyB) & 1) ^ 1
				cost := m + (exp0 ^ sym0) + (exp1 ^ sym1)
				if cost < newM[ns] {
					newM[ns] = cost
					prev[t][ns] = uint8(state)
					inBit[t][ns] = byte(bit)
				}
			}
		}
		oldM, newM = newM, oldM
	}
	nDataBits := nSym
	end := 0
	if flushed {
		nDataBits = nSym - (vConstraint - 1)
		end = 0
	} else {
		best := inf
		for s, m := range oldM {
			if m < best {
				best = m
				end = s
			}
		}
	}
	outBits := make([]byte, nDataBits)
	state := end
	for t := nSym - 1; t >= 0; t-- {
		bit := inBit[t][state]
		if t < nDataBits {
			outBits[t] = bit
		}
		state = int(prev[t][state])
	}
	out := make([]byte, (nDataBits+7)/8)
	for i, b := range outBits {
		out[i/8] |= b << uint(7-i%8)
	}
	return out[:nDataBits/8]
}
