package rs41

// Whiten применяет XOR-маску на месте (снятие и наложение совпадают).
func Whiten(b []byte) {
	for i := range b {
		b[i] ^= WhiteningMask[i%len(WhiteningMask)]
	}
}

// Descramble копирует кадр с линии и снимает whitening.
func Descramble(onAir []byte) []byte {
	out := append([]byte(nil), onAir...)
	Whiten(out)
	return out
}
