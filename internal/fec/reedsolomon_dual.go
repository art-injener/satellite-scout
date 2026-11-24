package fec

import "fmt"

// EncodeRSDual — RS(255,223) в dual-basis CCSDS (как encode_rs_ccsds).
// Нужен для USP и части CCSDS-кадров.
func EncodeRSDual(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > RSDataLen {
		return nil, fmt.Errorf("fec: RS dual data length %d", len(data))
	}
	conv := make([]byte, len(data))
	for i, b := range data {
		conv[i] = tal1tab[b]
	}
	parity, err := EncodeRS(conv)
	if err != nil {
		return nil, err
	}
	for i, b := range parity {
		parity[i] = taltab[b]
	}
	return parity, nil
}

// DecodeRSDual — dual-basis блок [данные|чётность] → данные.
func DecodeRSDual(block []byte) (data []byte, corrected int, err error) {
	n := len(block)
	if n <= RSParityLen || n > RSNN {
		return nil, 0, fmt.Errorf("fec: RS dual block length %d", n)
	}
	conv := make([]byte, n)
	for i, b := range block {
		conv[i] = tal1tab[b]
	}
	out, corr, err := DecodeRS(conv)
	if err != nil {
		return nil, 0, err
	}
	for i, b := range out {
		out[i] = taltab[b]
	}
	return out, corr, nil
}
