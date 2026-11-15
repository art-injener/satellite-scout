package freqplan

import (
	"errors"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SourceHeavensAbove — страница AmateurSats.aspx на heavens-above.com.
const SourceHeavensAbove = "heavens-above"

var (
	// ErrHeavensAboveLayout — на странице нет таблицы пролётов: сменилась вёрстка или страница-заглушка.
	ErrHeavensAboveLayout = errors.New("freqplan: heavens-above: pass table not found")
	// ErrHeavensAboveHTTP — сервер ответил не 200.
	ErrHeavensAboveHTTP = errors.New("freqplan: heavens-above: bad http status")
)

var (
	haRowRe   = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	haCellRe  = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	haTagRe   = regexp.MustCompile(`<[^>]+>`)
	haSatIDRe = regexp.MustCompile(`satid=(\d+)`)
	haLocRe   = regexp.MustCompile(`lat=(-?[\d.]+)&(?:amp;)?lng=(-?[\d.]+)`)
)

// Колонки строки пролёта: имя, дата, начало, высшая точка (высота, азимут), конец, частоты.
const (
	haColDate  = 1
	haColStart = 2
	haColMaxEl = 3
	haColEnd   = 5
	haPassCols = 7
)

// HAPass — пролёт из таблицы Heavens-Above, время в UTC.
type HAPass struct {
	NoradID  int
	Start    time.Time
	End      time.Time
	MaxElDeg float64
}

// HeavensAbove — Source из таблицы пролётов любительских КА Heavens-Above.
// В таблице только КА, пролетающие над станцией в ближайшие сутки.
type HeavensAbove struct {
	byNorad  map[int][]Tx
	passes   []HAPass
	lat, lon float64
	hasLoc   bool
}

// Transmitters — реализация Source.
func (h *HeavensAbove) Transmitters(noradID int) []Tx {
	if h == nil {
		return nil
	}
	return h.byNorad[noradID]
}

// Passes — все пролёты страницы в порядке таблицы.
func (h *HeavensAbove) Passes() []HAPass {
	if h == nil {
		return nil
	}
	return h.passes
}

// Location — координаты, для которых посчитана страница (из ссылок на КА).
func (h *HeavensAbove) Location() (lat, lon float64, ok bool) {
	if h == nil {
		return 0, 0, false
	}
	return h.lat, h.lon, h.hasLoc
}

// ParseHeavensAbove разбирает HTML AmateurSats.aspx. Строка таблицы — пролёт:
// имя (ссылка satinfo.aspx?satid=NORAD), время и высота пролёта, последняя колонка — downlink, МГц.
// Частоты КА берём из первой его строки, пролёты — из всех.
// В датах страницы нет года: его даёт fetchedAt, момент загрузки страницы.
func ParseHeavensAbove(r io.Reader, fetchedAt time.Time) (*HeavensAbove, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	page := string(data)
	start := strings.Index(page, `class="standardTable"`)
	if start < 0 {
		return nil, ErrHeavensAboveLayout
	}
	table := page[start:]
	if end := strings.Index(table, "</table>"); end >= 0 {
		table = table[:end]
	}

	out := &HeavensAbove{byNorad: make(map[int][]Tx)}
	for _, row := range haRowRe.FindAllStringSubmatch(table, -1) {
		m := haSatIDRe.FindStringSubmatch(row[1])
		if m == nil {
			continue
		}
		norad, convErr := strconv.Atoi(m[1])
		if convErr != nil || norad <= 0 {
			continue
		}
		if !out.hasLoc {
			out.lat, out.lon, out.hasLoc = parseHALocation(row[1])
		}
		cells := haCellRe.FindAllStringSubmatch(row[1], -1)
		if p, ok := parseHAPass(norad, cells, fetchedAt); ok {
			out.passes = append(out.passes, p)
		}
		if _, seen := out.byNorad[norad]; seen {
			continue
		}
		if len(cells) < 2 {
			continue
		}
		name := haText(cells[0][1])
		freqs, mode := parseHAFreqField(haText(cells[len(cells)-1][1]))
		txs := make([]Tx, 0, len(freqs))
		for _, hz := range freqs {
			txs = append(txs, Tx{Name: name, FreqHz: hz, Mode: mode, Source: SourceHeavensAbove})
		}
		out.byNorad[norad] = txs
	}
	return out, nil
}

func parseHALocation(row string) (lat, lon float64, ok bool) {
	m := haLocRe.FindStringSubmatch(row)
	if m == nil {
		return 0, 0, false
	}
	lat, latErr := strconv.ParseFloat(m[1], 64)
	lon, lonErr := strconv.ParseFloat(m[2], 64)
	return lat, lon, latErr == nil && lonErr == nil
}

// parseHAPass собирает пролёт из ячеек строки: «12 Nov», «14:30:01», «45°», …, «14:37:58».
func parseHAPass(norad int, cells [][]string, fetchedAt time.Time) (HAPass, bool) {
	if len(cells) < haPassCols {
		return HAPass{}, false
	}
	date := haText(cells[haColDate][1])
	start, err := parseHATime(date, haText(cells[haColStart][1]), fetchedAt)
	if err != nil {
		return HAPass{}, false
	}
	end, err := parseHATime(date, haText(cells[haColEnd][1]), fetchedAt)
	if err != nil {
		return HAPass{}, false
	}
	// Дата в строке — дата начала; пролёт через полночь кончается на следующий день.
	if end.Before(start) {
		end = end.AddDate(0, 0, 1)
	}
	maxEl, err := strconv.ParseFloat(strings.TrimSuffix(haText(cells[haColMaxEl][1]), "°"), 64)
	if err != nil {
		return HAPass{}, false
	}
	return HAPass{NoradID: norad, Start: start, End: end, MaxElDeg: maxEl}, true
}

// parseHATime — дата без года и время UTC; год выбирается ближайший к fetchedAt.
func parseHATime(date, clock string, fetchedAt time.Time) (time.Time, error) {
	year := fetchedAt.UTC().Year()
	t, err := time.Parse("2 Jan 2006 15:04:05", date+" "+strconv.Itoa(year)+" "+clock)
	if err != nil {
		return time.Time{}, err
	}
	const halfYear = 183 * 24 * time.Hour
	switch {
	case t.Sub(fetchedAt) > halfYear:
		t = t.AddDate(-1, 0, 0)
	case fetchedAt.Sub(t) > halfYear:
		t = t.AddDate(1, 0, 0)
	}
	return t, nil
}

// haText — текст ячейки без тегов и HTML-сущностей, пробелы схлопнуты.
func haText(cell string) string {
	return strings.Join(strings.Fields(html.UnescapeString(haTagRe.ReplaceAllString(cell, " "))), " ")
}

// parseHAFreqField разбирает «437.800», «137.100 / 137.900», «145.950 USB»,
// «29.354- 29.394» (полоса транспондера: середина) и сокращение «437.125/150» (437.125 и 437.150).
// Буквенное слово в конце — режим.
func parseHAFreqField(s string) ([]int64, string) {
	fields := strings.Fields(s)
	var mode []string
	for len(fields) > 0 && isWord(fields[len(fields)-1]) {
		mode = append([]string{fields[len(fields)-1]}, mode...)
		fields = fields[:len(fields)-1]
	}

	var out []int64
	prev := ""
	for part := range strings.SplitSeq(strings.Join(fields, " "), "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if lo, hi, ok := splitRange(part); ok {
			out = append(out, mhzToHz((lo+hi)/2))
			continue
		}
		if prev != "" && !strings.Contains(part, ".") && len(part) < len(prev) {
			part = prev[:len(prev)-len(part)] + part
		}
		if hz := parseMHzToken(part); hz > 0 {
			out = append(out, hz)
			prev = part
		}
	}
	return uniqueHz(out), strings.Join(mode, " ")
}

func isWord(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return s != ""
}
