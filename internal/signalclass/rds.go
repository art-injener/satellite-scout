package signalclass

import (
	"math"
	"strings"
	"unicode"
)

// RDS (IEC 62106): поднесущая 57 кГц, 1187,5 бит/с, дифференциально кодированная
// двухфазная BPSK. Имя службы PS — 8 символов из групп 0A/0B.

const (
	rdsBitRate   = 1187.5
	rdsCarrierHz = 57_000
	rdsPilotHz   = 19_000
	// rdsPoly — порождающий многочлен (26,16): x¹⁰+x⁸+x⁷+x⁵+x⁴+x³+1.
	rdsPoly = 0x5B9
	// rdsChunkSec — кусок дискриминатора для своей оценки тактов.
	// Слишком короткий на эфире даёт 0 групп; ~0,8 с держит фазу и набирает блоки.
	rdsChunkSec = 0.8
)

// Слова смещения блоков группы (IEC 62106): синдром верного блока равен слову.
var rdsOffset = [...]int{0x0FC, 0x198, 0x168, 0x1B4, 0x350} // A B C D C'

// decodeRDSPS — имя станции из IQ широкой ЧМ. Пусто, если пилота нет или PS не собрался.
func decodeRDSPS(iq []complex64, rate float64) string {
	ps, _, _, _, _, _ := rdsTry(iq, rate)
	return ps
}

// rdsTry — PS, сырые сегменты и счётчики для склейки между разборами и диагностики.
func rdsTry(iq []complex64, rate float64) (ps string, parts [4][2]byte, have [4]bool, nBits, nGroups, nSeg int) {
	if rate < 2*(rdsCarrierHz+3_000) || len(iq) < int(rate/2) {
		return "", parts, have, 0, 0, 0
	}
	disc := instFreq(iq, rate)
	// Несущая: 3×пилот (с допуском ppm), номинал 57 кГц и сетка ±200 Гц —
	// на эфире оценка пилота часто слегка врёт, и одна частота даёт 0 групп.
	carriers := []float64{rdsCarrierHz}
	if f, ratio := clockLine(disc, rate, rdsPilotHz-pilotTolHz, rdsPilotHz+pilotTolHz); ratio >= pilotMinRatio {
		carriers = []float64{3 * f, rdsCarrierHz, 3*f - 200, 3*f + 200}
	} else {
		carriers = []float64{rdsCarrierHz, rdsCarrierHz - 200, rdsCarrierHz + 200}
	}
	for _, carrier := range carriers {
		bits := rdsBits(disc, rate, carrier)
		if len(bits) > nBits {
			nBits = len(bits)
		}
		if len(bits) < 104 {
			continue
		}
		p, h, g := rdsCollect(bits)
		if g > nGroups {
			parts, have, nGroups = p, h, g
		}
		// Инверсия бит: на эфире полярность манчестера иногда наоборот.
		inv := make([]byte, len(bits))
		for i, b := range bits {
			inv[i] = b ^ 1
		}
		if p, h, g := rdsCollect(inv); g > nGroups {
			parts, have, nGroups = p, h, g
		}
		if nGroups > 0 && have[0] && have[1] && have[2] && have[3] {
			break
		}
	}
	for _, ok := range have {
		if ok {
			nSeg++
		}
	}
	if have[0] && have[1] && have[2] && have[3] {
		b := []byte{
			parts[0][0], parts[0][1], parts[1][0], parts[1][1],
			parts[2][0], parts[2][1], parts[3][0], parts[3][1],
		}
		ps = cleanPS(b)
	}
	return ps, parts, have, nBits, nGroups, nSeg
}

// rdsCollect — сегменты PS из потока бит и число групп с верным синдромом.
func rdsCollect(bits []byte) (parts [4][2]byte, have [4]bool, nGroups int) {
	for i := 0; i+104 <= len(bits); i++ {
		blks, ok := rdsReadGroup(bits[i : i+104])
		if !ok {
			continue
		}
		nGroups++
		b := blks[1]
		if (b>>12)&0xF != 0 {
			continue
		}
		seg := b & 3
		d := blks[3]
		parts[seg][0] = byte(d >> 8)
		parts[seg][1] = byte(d)
		have[seg] = true
	}
	return parts, have, nGroups
}

// rdsBits — дифференциальные биты RDS с выхода ЧМ-детектора.
func rdsBits(disc []float64, rate, carrierHz float64) []byte {
	iBB, qBB := rdsBasebandIQ(disc, rate, carrierHz)
	if len(iBB) < 64 {
		return nil
	}
	sps := rate / rdsBitRate
	if sps < 4 {
		return nil
	}
	half := int(math.Round(sps / 2))
	if half < 2 {
		return nil
	}
	// Куски по ~rdsChunkSec: на всём окне 2 с доля символа уплывает.
	chunk := int(math.Round(rate * rdsChunkSec))
	minChunk := int(math.Round(sps)) * 80
	if chunk < minChunk {
		chunk = minChunk
	}
	var out []byte
	for start := 0; start < len(iBB); {
		end := start + chunk
		if end > len(iBB) {
			end = len(iBB)
		}
		part := rdsBitsFromBB(iBB[start:end], qBB[start:end], sps, half)
		if len(out) == 0 {
			out = append(out, part...)
		} else if len(part) > 1 {
			out = append(out, part[1:]...)
		}
		if end == len(iBB) {
			break
		}
		// Перекрытие на символ: стык кусков меньше рвёт группы.
		start = end - int(math.Round(sps))
		if start <= 0 {
			start = end
		}
	}
	return out
}

func rdsBitsFromBB(iBB, qBB []float64, sps float64, half int) []byte {
	nSym := int(float64(len(iBB))/sps) - 1
	if nSym < 32 {
		return nil
	}
	best, bestPh, useQ := 0.0, 0.0, false
	step := max(1, int(sps)/8)
	for _, q := range []bool{false, true} {
		x := iBB
		if q {
			x = qBB
		}
		for k := 0; k < int(sps); k += step {
			ph := float64(k)
			var e float64
			for i := 0; i < nSym; i++ {
				e += math.Abs(rdsCorr(x, int(math.Round(ph+float64(i)*sps)), half))
			}
			if e > best {
				best, bestPh, useQ = e, ph, q
			}
		}
	}
	// Уточнение фазы вокруг грубого максимума.
	lo := int(bestPh) - step
	hi := int(bestPh) + step
	x := iBB
	if useQ {
		x = qBB
	}
	for k := max(0, lo); k <= min(int(sps)-1, hi); k++ {
		ph := float64(k)
		var e float64
		for i := 0; i < nSym; i++ {
			e += math.Abs(rdsCorr(x, int(math.Round(ph+float64(i)*sps)), half))
		}
		if e > best {
			best, bestPh = e, ph
		}
	}
	raw := make([]byte, nSym)
	for i := range raw {
		if rdsCorr(x, int(math.Round(bestPh+float64(i)*sps)), half) >= 0 {
			raw[i] = 1
		}
	}
	out := make([]byte, len(raw)-1)
	for i := range out {
		out[i] = raw[i+1] ^ raw[i]
	}
	return out
}

// rdsCorr — корреляция отрезка с двухфазным шаблоном (+half, −half).
func rdsCorr(x []float64, start, half int) float64 {
	if start+2*half > len(x) {
		return 0
	}
	var s float64
	for i := 0; i < half; i++ {
		s += x[start+i]
	}
	for i := 0; i < half; i++ {
		s -= x[start+half+i]
	}
	return s
}

// rdsBasebandIQ — квадратурный перенос на carrierHz и ФНЧ ~2,4 кГц.
// Раньше срез был ~bitrate (1,2 кГц) — манчестер RDS до ~2,4 кГц обрезался, групп 0.
func rdsBasebandIQ(disc []float64, rate, carrierHz float64) (iOut, qOut []float64) {
	if carrierHz <= 0 {
		carrierHz = rdsCarrierHz
	}
	w := 2 * math.Pi * carrierHz / rate
	rawI := make([]float64, len(disc))
	rawQ := make([]float64, len(disc))
	for i, v := range disc {
		c, s := math.Sincos(w * float64(i))
		rawI[i] = v * c
		rawQ[i] = v * s
	}
	n := max(3, int(math.Round(rate/(2*2_400))))
	return movingAvg(rawI, n), movingAvg(rawQ, n)
}

// rdsSyndrome — остаток 26-битного блока по rdsPoly (IEC 62106).
// У верного блока равна слову смещения A/B/C/C′/D.
func rdsSyndrome(block26 int) int {
	reg := 0
	for i := 25; i >= 0; i-- {
		reg = (reg << 1) | ((block26 >> i) & 1)
		if reg&0x400 != 0 {
			reg ^= rdsPoly
		}
	}
	return reg & 0x3FF
}

// rdsPSFromBits собирает PS из групп с верными блоками A–D.
func rdsPSFromBits(bits []byte) string {
	parts, have, _ := rdsCollect(bits)
	if !(have[0] && have[1] && have[2] && have[3]) {
		return ""
	}
	b := []byte{
		parts[0][0], parts[0][1], parts[1][0], parts[1][1],
		parts[2][0], parts[2][1], parts[3][0], parts[3][1],
	}
	return cleanPS(b)
}

// rdsReadGroup — четыре блока по 26 бит; смещения A,B,C|C′,D.
func rdsReadGroup(bits []byte) ([4]int, bool) {
	var out [4]int
	want := [][]int{{0}, {1}, {2, 4}, {3}} // индексы rdsOffset
	for b := 0; b < 4; b++ {
		var block int
		for i := 0; i < 26; i++ {
			block = (block << 1) | int(bits[b*26+i]&1)
		}
		s := rdsSyndrome(block)
		ok := false
		for _, oi := range want[b] {
			if s == rdsOffset[oi] {
				ok = true
				break
			}
		}
		if !ok {
			return out, false
		}
		out[b] = (block >> 10) & 0xFFFF
	}
	return out, true
}

// cleanPS — печатные символы PS, края обрезаны; пустое или мусор — "".
func cleanPS(b []byte) string {
	for i, c := range b {
		if c < 0x20 || c > 0x7E {
			b[i] = ' '
		}
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters++
		}
	}
	if letters < 2 {
		return ""
	}
	return s
}

// MergeRDSParts дописывает сегменты PS из свежего разбора в накопленные.
// Возвращает имя, если все четыре сегмента уже есть.
func MergeRDSParts(parts *[4][2]byte, have *[4]bool, addParts [4][2]byte, addHave [4]bool) string {
	for i := 0; i < 4; i++ {
		if addHave[i] {
			parts[i] = addParts[i]
			have[i] = true
		}
	}
	if !(have[0] && have[1] && have[2] && have[3]) {
		return ""
	}
	b := []byte{
		parts[0][0], parts[0][1], parts[1][0], parts[1][1],
		parts[2][0], parts[2][1], parts[3][0], parts[3][1],
	}
	return cleanPS(b)
}

// makeRDSBlock — 26-битный блок для тестов: 16 бит данных и слово смещения.
func makeRDSBlock(info16, offsetIdx int) int {
	off := rdsOffset[offsetIdx]
	// check = syndrome(info<<10) ⊕ offset.
	chk := rdsSyndrome(info16<<10) ^ off
	return (info16 << 10) | chk
}
