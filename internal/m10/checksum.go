package m10

// updateCheck — шаг линейного checksum M10 (rs1729 / m10mod.c).
func updateCheck(c int, b byte) int {
	c1 := c & 0xFF
	b = (b >> 1) | ((b & 1) << 7)
	b ^= (b >> 2) & 0xFF
	t6 := (c & 1) ^ ((c >> 2) & 1) ^ ((c >> 4) & 1)
	t7 := ((c >> 1) & 1) ^ ((c >> 3) & 1) ^ ((c >> 5) & 1)
	t := (c & 0x3F) | (t6 << 6) | (t7 << 7)
	s := (c >> 7) & 0xFF
	s ^= (s >> 2) & 0xFF
	c0 := int(b) ^ t ^ s
	return ((c1 << 8) | (c0 & 0xFF)) & 0xFFFF
}

// Checksum — контрольная сумма по len байтам (обычно до posCheck).
func Checksum(msg []byte, length int) uint16 {
	cs := 0
	for i := 0; i < length && i < len(msg); i++ {
		cs = updateCheck(cs, msg[i])
	}
	return uint16(cs & 0xFFFF)
}

// FrameOK — тип M10 и checksum.
func FrameOK(frame []byte) bool {
	if len(frame) < FrameLen {
		return false
	}
	if frame[0] != StdLenByte || frame[1] != TypeM10 {
		return false
	}
	want := uint16(frame[posCheck])<<8 | uint16(frame[posCheck+1])
	return Checksum(frame, posCheck) == want
}

// SealChecksum записывает checksum в хвост кадра.
func SealChecksum(frame []byte) {
	if len(frame) < FrameLen {
		return
	}
	cs := Checksum(frame, posCheck)
	frame[posCheck] = byte(cs >> 8)
	frame[posCheck+1] = byte(cs)
}
