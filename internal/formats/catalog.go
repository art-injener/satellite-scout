// Package formats — каталог карточек КА (ADR-009 §4.2): позывные, закреплённый профиль, декодеры.
package formats

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

//go:embed catalog.json
var embeddedCatalog []byte

// Card — карточка КА в каталоге форматов.
type Card struct {
	Norad        int           `json:"norad"`
	Name         string        `json:"name"`
	Transmitters []Transmitter `json:"transmitters"`
}

// Transmitter — передатчик на карточке: частота, профиль, позывные.
type Transmitter struct {
	FreqHz      uint64   `json:"freq_hz"`
	Profile     string   `json:"profile"`
	BandwidthHz uint32   `json:"bandwidth_hz"`
	Decoders    []string `json:"decoders"`
	Callsigns   []string `json:"callsigns"`
}

// Catalog — загруженные карточки и индекс позывной → NORAD.
type Catalog struct {
	mu        sync.RWMutex
	cards     []Card
	byCall    map[string]callEntry
	byNorad   map[int]Card
}

type callEntry struct {
	Norad int
	Name  string
}

// Load читает встроенный каталог и накладывает оверрайд из path (если файл есть).
// Пустой path — только встроенный.
func Load(overridePath string) (*Catalog, error) {
	cards, err := parseCards(embeddedCatalog)
	if err != nil {
		return nil, fmt.Errorf("formats: embedded catalog: %w", err)
	}
	if overridePath != "" {
		raw, err := os.ReadFile(overridePath)
		if err == nil {
			extra, err := parseCards(raw)
			if err != nil {
				return nil, fmt.Errorf("formats: %s: %w", overridePath, err)
			}
			cards = mergeCards(cards, extra)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("formats: read %s: %w", overridePath, err)
		}
	}
	return newCatalog(cards), nil
}

func parseCards(raw []byte) ([]Card, error) {
	var cards []Card
	if err := json.Unmarshal(raw, &cards); err != nil {
		return nil, err
	}
	return cards, nil
}

// mergeCards: оверрайд с тем же NORAD заменяет карточку; новые NORAD добавляются.
func mergeCards(base, override []Card) []Card {
	byID := make(map[int]Card, len(base)+len(override))
	order := make([]int, 0, len(base)+len(override))
	for _, c := range base {
		if _, ok := byID[c.Norad]; !ok {
			order = append(order, c.Norad)
		}
		byID[c.Norad] = c
	}
	for _, c := range override {
		if _, ok := byID[c.Norad]; !ok {
			order = append(order, c.Norad)
		}
		byID[c.Norad] = c
	}
	out := make([]Card, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

func newCatalog(cards []Card) *Catalog {
	c := &Catalog{
		cards:   cards,
		byCall:  make(map[string]callEntry),
		byNorad: make(map[int]Card, len(cards)),
	}
	for _, card := range cards {
		c.byNorad[card.Norad] = card
		for _, tx := range card.Transmitters {
			for _, cs := range tx.Callsigns {
				key := NormalizeCallsign(cs)
				if key == "" {
					continue
				}
				if prev, ok := c.byCall[key]; ok && prev.Norad != card.Norad {
					// Два КА с одним позывным: индекс не однозначен — убираем ключ.
					delete(c.byCall, key)
					continue
				}
				c.byCall[key] = callEntry{Norad: card.Norad, Name: card.Name}
			}
		}
	}
	return c
}

// NormalizeCallsign — база позывного без SSID, верхний регистр.
func NormalizeCallsign(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// LookupCallsign ищет КА по позывному из кадра; ok=false — нет или неоднозначно.
func (c *Catalog) LookupCallsign(call string) (norad int, name string, ok bool) {
	if c == nil {
		return 0, "", false
	}
	key := NormalizeCallsign(call)
	if key == "" || skipCallsign(key) {
		return 0, "", false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.byCall[key]
	if !ok {
		return 0, "", false
	}
	return e.Norad, e.Name, true
}

func skipCallsign(call string) bool {
	switch call {
	case "BEACON", "CQ", "IDENT", "TEST":
		return true
	default:
		return false
	}
}

// CardByNorad возвращает карточку КА.
func (c *Catalog) CardByNorad(norad int) (Card, bool) {
	if c == nil {
		return Card{}, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	card, ok := c.byNorad[norad]
	return card, ok
}

// Cards — копия списка карточек.
func (c *Catalog) Cards() []Card {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Card, len(c.cards))
	copy(out, c.cards)
	return out
}

// profilePinFloorHz — минимальный допуск по частоте, если у передатчика узкая или нулевая полоса.
const profilePinFloorHz = 5_000

// ProfileFor — закреплённый профиль с карточки КА по NORAD и частоте канала.
// Берётся ближайший передатчик с непустым profile, если |Δf| ≤ max(bandwidth_hz/2, 5 кГц).
func (c *Catalog) ProfileFor(norad int, freqHz uint64) (profile string, ok bool) {
	card, ok := c.CardByNorad(norad)
	if !ok {
		return "", false
	}
	var best string
	var bestDelta uint64
	found := false
	for _, tx := range card.Transmitters {
		if tx.Profile == "" {
			continue
		}
		delta := absDiffU64(tx.FreqHz, freqHz)
		half := uint64(tx.BandwidthHz) / 2
		if half < profilePinFloorHz {
			half = profilePinFloorHz
		}
		if delta > half {
			continue
		}
		if !found || delta < bestDelta {
			best = tx.Profile
			bestDelta = delta
			found = true
		}
	}
	return best, found
}

func absDiffU64(a, b uint64) uint64 {
	if a >= b {
		return a - b
	}
	return b - a
}
