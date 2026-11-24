package rs41

// CorrectFrame применяет dual interleaved RS(255,231) к кадру после whitening.
// Чётность в кадре — reverse(encode_parity), данные чётных/нечётных байт с конца
// области данных (как в rs1729). Пишет исправления обратно в frame.
// ok=false, если хотя бы одно кодовое слово не исправлено.
func CorrectFrame(frame []byte) (corrected int, ok bool) {
	if len(frame) < StdFrameLen {
		return 0, false
	}
	// Область данных до конца расширенного кадра (нули после std).
	ext := make([]byte, ExtFrameLen)
	copy(ext, frame)
	data := ext[DataOffset:] // 462 байт

	msg1 := make([]byte, rsDataLen)
	msg2 := make([]byte, rsDataLen)
	// 132 полезных байта с конца data[0:264] шагом 2, затем нули (укорочение).
	const useful = (StdFrameLen - DataOffset) / 2 // 132
	for i := 0; i < useful; i++ {
		msg1[i] = data[2*(useful-1-i)]
		msg2[i] = data[2*(useful-1-i)+1]
	}

	par1 := reverseCopy(frame[HeaderLen : HeaderLen+rsParityLen])
	par2 := reverseCopy(frame[HeaderLen+rsParityLen : HeaderLen+ParityLen])

	block1 := append(append([]byte{}, msg1[:useful]...), par1...)
	block2 := append(append([]byte{}, msg2[:useful]...), par2...)

	d1, c1, err1 := decodeBlock(block1)
	d2, c2, err2 := decodeBlock(block2)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	corrected = c1 + c2

	// Записать исправленные данные обратно (обратный порядок).
	for i := 0; i < useful; i++ {
		data[2*(useful-1-i)] = d1[i]
		data[2*(useful-1-i)+1] = d2[i]
	}
	copy(frame[DataOffset:], data[:len(frame)-DataOffset])

	// Обновить чётность (на случай повторной проверки).
	if enc, err := encodeMsg(d1); err == nil {
		copy(frame[HeaderLen:HeaderLen+rsParityLen], reverseCopy(enc[len(d1):]))
	}
	if enc, err := encodeMsg(d2); err == nil {
		copy(frame[HeaderLen+rsParityLen:HeaderLen+ParityLen], reverseCopy(enc[len(d2):]))
	}
	return corrected, true
}

func reverseCopy(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[i] = b[len(b)-1-i]
	}
	return out
}

// FrameOK — после ECC хотя бы блок статуса с верной CRC.
func FrameOK(frame []byte) bool {
	return len(frame) >= StdFrameLen && checkTLV(frame, posStatus)
}
