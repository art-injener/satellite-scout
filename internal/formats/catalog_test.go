package formats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_EmbeddedGEOSCAN(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	norad, name, ok := c.LookupCallsign("RS23S")
	if !ok || norad != 53385 || name != "GEOSCAN-EDELVEIS" {
		t.Fatalf("RS23S → %d %q ok=%v", norad, name, ok)
	}
	norad, _, ok = c.LookupCallsign("rs20s-1")
	if !ok || norad != 53385 {
		t.Fatalf("rs20s-1 → %d ok=%v", norad, ok)
	}
	if _, _, ok := c.LookupCallsign("BEACON"); ok {
		t.Fatal("BEACON must not bind")
	}
	if _, _, ok := c.LookupCallsign("CQ"); ok {
		t.Fatal("CQ must not bind")
	}
}

func TestLoad_OverrideAddsCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "formats.json")
	body := `[{"norad":25544,"name":"ISS","transmitters":[{"freq_hz":145825000,"callsigns":["RS0ISS"]}]}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	norad, name, ok := c.LookupCallsign("RS0ISS")
	if !ok || norad != 25544 || name != "ISS" {
		t.Fatalf("got %d %q ok=%v", norad, name, ok)
	}
	if _, _, ok := c.LookupCallsign("RS23S"); !ok {
		t.Fatal("embedded GEOSCAN must remain")
	}
}

func TestLoad_AmbiguousCallsignDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "formats.json")
	body := `[
	  {"norad":1,"name":"A","transmitters":[{"callsigns":["DUP"]}]},
	  {"norad":2,"name":"B","transmitters":[{"callsigns":["DUP"]}]}
	]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.LookupCallsign("DUP"); ok {
		t.Fatal("ambiguous callsign must not resolve")
	}
}

func TestNormalizeCallsign(t *testing.T) {
	if got := NormalizeCallsign(" rs23s-0 "); got != "RS23S" {
		t.Errorf("got %q", got)
	}
}

func TestCard_GEOSCANMatchesADR(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	card, ok := c.CardByNorad(53385)
	if !ok {
		t.Fatal("GEOSCAN card missing")
	}
	if card.Name != "GEOSCAN-EDELVEIS" || len(card.Transmitters) != 1 {
		t.Fatalf("card=%+v", card)
	}
	tx := card.Transmitters[0]
	if tx.FreqHz != 436_200_000 || tx.Profile != "fsk9600_geoscan" || tx.BandwidthHz != 20_000 {
		t.Fatalf("tx=%+v", tx)
	}
	if len(tx.Decoders) != 2 || tx.Decoders[0] != "geoscan.beacon" || tx.Decoders[1] != "geoscan.image" {
		t.Fatalf("decoders=%v", tx.Decoders)
	}
	if len(tx.Callsigns) < 1 {
		t.Fatal("callsigns required")
	}
}

func TestProfileFor_PinnedNearFreq(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.ProfileFor(53385, 436_200_000)
	if !ok || got != "fsk9600_geoscan" {
		t.Fatalf("exact: %q ok=%v", got, ok)
	}
	got, ok = c.ProfileFor(53385, 436_205_000)
	if !ok || got != "fsk9600_geoscan" {
		t.Fatalf("within half band: %q ok=%v", got, ok)
	}
	if _, ok := c.ProfileFor(53385, 436_250_000); ok {
		t.Fatal("far freq must not pin")
	}
	if _, ok := c.ProfileFor(25544, 145_800_000); ok {
		t.Fatal("unknown NORAD must not pin")
	}
}

func TestProfileFor_OverrideCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "formats.json")
	body := `[{
		"norad":25544,
		"name":"ISS",
		"transmitters":[{
			"freq_hz":145825000,
			"profile":"afsk1200",
			"bandwidth_hz":15000,
			"decoders":["ax25"],
			"callsigns":["RS0ISS"]
		}]
	}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.ProfileFor(25544, 145_825_000)
	if !ok || got != "afsk1200" {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
