package m10

const (
	FrameLen  = 101 // 0x64+1
	StdLenByte = 0x64
	TypeM10   = 0x9F
	Baud      = 9615

	posLat   = 0x0E
	posLon   = 0x12
	posAlt   = 0x16
	posSN    = 0x5D
	posCheck = 0x63 // два байта BE checksum

	headerBits = 16 // 0x64 0x9F
)

// b60b60 — масштаб широты/долготы Trimble (2^30/90).
const b60b60 = float64(uint64(1)<<30) / 90.0
