package signals

import "testing"

func TestDefault_KnownServices(t *testing.T) {
	got := map[string]Hit{}
	for _, h := range Match(Default(), Input{}) {
		got[h.ID] = h
	}
	if len(got) != 0 {
		t.Fatalf("empty observation matched %#v", got)
	}

	voice := Match(Default(), Input{
		Modulation: "fm_voice", CenterHz: 145_500_000, OccupiedHz: 12_000, Emission: "pulsed",
	})
	if len(voice) != 1 || voice[0].ID != IDAmateur || voice[0].Label != "Радиопередатчик" || voice[0].Probe != ProbeNone {
		t.Fatalf("amateur: %#v", voice)
	}
	if n := Match(Default(), Input{
		Modulation: "fm_voice", CenterHz: 446_100_000, OccupiedHz: 12_000, Emission: "pulsed",
	}); len(n) != 0 {
		t.Fatalf("PMR matched amateur: %#v", n)
	}
	if n := Match(Default(), Input{
		Modulation: "fm_voice", CenterHz: 145_500_000, OccupiedHz: 16_900, Emission: "undetermined",
	}); len(n) != 1 || n[0].ID != IDAmateur {
		t.Fatalf("voice before the emission is known: %#v", n)
	}
	if n := Match(Default(), Input{
		Modulation: "fm_voice", CenterHz: 145_500_000, OccupiedHz: 12_000, Emission: "continuous",
	}); len(n) != 1 || n[0].ID != IDAmateur {
		t.Fatalf("long voice exchange: %#v", n)
	}
	if n := Match(Default(), Input{
		Modulation: "fm", CenterHz: 435_765_000, OccupiedHz: 15_000,
	}); len(n) != 1 || n[0].ID != IDAmateur {
		t.Fatalf("amateur FM without speech: %#v", n)
	}
	if n := Match(Default(), Input{
		Modulation: "fm_voice", CenterHz: 145_500_000, OccupiedHz: 30_000,
	}); len(n) != 0 {
		t.Fatalf("voice wider than a channel matched: %#v", n)
	}
	data := Match(Default(), Input{
		Modulation: "2fsk", CenterHz: 436_276_000, OccupiedHz: 11_300, Baud: 9600,
	})
	if len(data) != 1 || data[0].ID != IDAmateurData || data[0].Label != "Радиопередатчик" {
		t.Fatalf("amateur data: %#v", data)
	}

	key := Match(Default(), Input{
		Modulation: "ook", CenterHz: 433_920_000, Baud: 2_000, Emission: "pulsed",
		Bursts: 4, OnS: 1.2,
	})
	if len(key) != 1 || key[0].ID != IDISM || key[0].Probe != ProbeNone {
		t.Fatalf("ism: %#v", key)
	}
	if n := Match(Default(), Input{
		Modulation: "2fsk", CenterHz: 433_920_000, Baud: 2_000, Emission: "pulsed",
		Bursts: 1, OnS: 0.3,
	}); len(n) != 0 {
		t.Fatalf("single burst matched ism: %#v", n)
	}

	fm := Match(Default(), Input{Modulation: "wfm", CenterHz: 104_100_000})
	if len(fm) != 1 || fm[0].ID != IDBroadcast || fm[0].Label != "Эфирное радио" {
		t.Fatalf("broadcast: %#v", fm)
	}

	lora := Match(Default(), Input{Modulation: "lfm", CenterHz: 868_100_000, OccupiedHz: 250_000})
	if len(lora) != 1 || lora[0].ID != IDLoRa || lora[0].Probe != ProbeRequired {
		t.Fatalf("lora: %#v", lora)
	}
	gsm := Match(Default(), Input{Modulation: "2fsk", CenterHz: 940_000_000, OccupiedHz: 200_000})
	if len(gsm) != 1 || gsm[0].ID != IDGSM || gsm[0].Probe != ProbeRequired {
		t.Fatalf("gsm: %#v", gsm)
	}
	if n := Match(Default(), Input{Modulation: "2fsk", CenterHz: 437_000_000, OccupiedHz: 200_000}); len(n) != 0 {
		t.Fatalf("wide 2fsk outside GSM matched: %#v", n)
	}
}

func TestMatch_SeveralHitsStayUnresolved(t *testing.T) {
	cat := []Signature{
		{ID: "a", Label: "A", Modulations: []string{"fm"}, Probe: ProbeNone},
		{ID: "b", Label: "B", Modulations: []string{"fm"}, Probe: ProbeRequired},
	}
	hits := Match(cat, Input{Modulation: "fm"})
	if len(hits) != 2 {
		t.Fatalf("hits %#v, want both", hits)
	}
}

func TestMerge_ReplacesByID(t *testing.T) {
	base := []Signature{{ID: "broadcast", Label: "Эфирное радио"}, {ID: "lora", Label: "LoRa"}}
	over, err := Parse([]byte(`[{"id":"broadcast","label":"FM","modulations":["wfm"],"probe":"none"},{"id":"extra","label":"X","modulations":["cw"],"probe":"none"}]`))
	if err != nil {
		t.Fatal(err)
	}
	got := Merge(base, over)
	if len(got) != 3 || got[0].Label != "FM" || got[1].ID != "lora" || got[2].ID != "extra" {
		t.Fatalf("merged %#v", got)
	}
}

func TestParse_RejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte(`{`)); err == nil {
		t.Fatal("expected an error")
	}
}
