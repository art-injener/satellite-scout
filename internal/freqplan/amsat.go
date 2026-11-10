package freqplan

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// SourceAMSAT — зеркало JE9PEL/AMSAT (palewire CSV).
const SourceAMSAT = "amsat"

// amsatActive — статусы, которые берём в план.
var amsatActive = map[string]bool{
	"active":      true,
	"operational": true,
}

// AMSAT — Source из CSV AMSAT/JE9PEL.
type AMSAT struct {
	byNorad map[int][]Tx
}

// Transmitters — реализация Source.
func (a *AMSAT) Transmitters(noradID int) []Tx {
	if a == nil {
		return nil
	}
	return a.byNorad[noradID]
}

// ParseAMSAT читает CSV зеркала (amsat-active / amsat-all).
// Колонки: name, norad_id, uplink, downlink, beacon, mode, callsign, status, …
// В план идут downlink и beacon активных КА; диапазон «a-b» — середина.
func ParseAMSAT(r io.Reader) (*AMSAT, error) {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("freqplan: amsat header: %w", err)
	}
	col := amsatColumns(header)
	if col.norad < 0 || col.downlink < 0 || col.status < 0 {
		return nil, fmt.Errorf("freqplan: amsat: need norad_id, downlink, status columns")
	}
	out := &AMSAT{byNorad: make(map[int][]Tx)}
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("freqplan: amsat row: %w", err)
		}
		status := strings.ToLower(strings.TrimSpace(cell(row, col.status)))
		if !amsatActive[status] {
			continue
		}
		norad, err := strconv.Atoi(strings.TrimSpace(cell(row, col.norad)))
		if err != nil || norad <= 0 {
			continue
		}
		name := strings.TrimSpace(cell(row, col.name))
		mode := strings.TrimSpace(cell(row, col.mode))
		freqs := parseAMSATFreqField(cell(row, col.downlink))
		freqs = append(freqs, parseAMSATFreqField(cell(row, col.beacon))...)
		for _, hz := range uniqueHz(freqs) {
			out.byNorad[norad] = append(out.byNorad[norad], Tx{
				Name: name, FreqHz: hz, Mode: mode, Source: SourceAMSAT,
			})
		}
	}
	return out, nil
}

type amsatCol struct {
	name, norad, downlink, beacon, mode, status int
}

func amsatColumns(header []string) amsatCol {
	c := amsatCol{name: -1, norad: -1, downlink: -1, beacon: -1, mode: -1, status: -1}
	for i, h := range header {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "name":
			c.name = i
		case "norad_id", "norad":
			c.norad = i
		case "downlink":
			c.downlink = i
		case "beacon":
			c.beacon = i
		case "mode":
			c.mode = i
		case "status":
			c.status = i
		}
	}
	return c
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// parseAMSATFreqField разбирает «436.795», «435.4/436.2», «435.670-435.610» (МГц → Гц).
func parseAMSATFreqField(s string) []int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []int64
	for _, part := range strings.Split(s, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := splitRange(part); ok {
			if hz := mhzToHz((lo + hi) / 2); hz > 0 {
				out = append(out, hz)
			}
			continue
		}
		if hz := parseMHzToken(part); hz > 0 {
			out = append(out, hz)
		}
	}
	return out
}

func splitRange(s string) (lo, hi float64, ok bool) {
	i := strings.IndexByte(s, '-')
	// Частоты вида 29.400- 29.500 с пробелом; не путать с минусом в начале.
	if i <= 0 || i == len(s)-1 {
		return 0, 0, false
	}
	// Вторая часть диапазона тоже может начинаться с цифры после '-'.
	a := strings.TrimSpace(s[:i])
	b := strings.TrimSpace(s[i+1:])
	lo, err1 := strconv.ParseFloat(a, 64)
	hi, err2 := strconv.ParseFloat(b, 64)
	if err1 != nil || err2 != nil || lo <= 0 || hi <= 0 {
		return 0, 0, false
	}
	if hi < lo {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

func parseMHzToken(s string) int64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "*")
	mhz, err := strconv.ParseFloat(s, 64)
	if err != nil || mhz <= 0 {
		return 0
	}
	return mhzToHz(mhz)
}

func mhzToHz(mhz float64) int64 {
	return int64(mhz*1e6 + 0.5)
}

func uniqueHz(in []int64) []int64 {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(in))
	var out []int64
	for _, hz := range in {
		if seen[hz] {
			continue
		}
		seen[hz] = true
		out = append(out, hz)
	}
	return out
}
