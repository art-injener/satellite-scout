package rs41

// CRC16 — CRC-CCITT (poly 0x1021, init 0xFFFF) по данным TLV-блока.
func CRC16(data []byte) uint16 {
	rem := uint16(0xFFFF)
	for _, b := range data {
		rem ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if rem&0x8000 != 0 {
				rem = (rem << 1) ^ 0x1021
			} else {
				rem <<= 1
			}
		}
	}
	return rem
}

// checkTLV сверяет CRC блока в pos: [id][len][data len][crc16 LE].
func checkTLV(frame []byte, pos int) bool {
	if pos+4 > len(frame) {
		return false
	}
	n := int(frame[pos+1])
	if pos+2+n+2 > len(frame) {
		return false
	}
	body := frame[pos+2 : pos+2+n]
	want := uint16(frame[pos+2+n]) | uint16(frame[pos+3+n])<<8
	return CRC16(body) == want
}
