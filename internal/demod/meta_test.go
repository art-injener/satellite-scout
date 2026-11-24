package demod

import (
	"slices"
	"testing"

	"github.com/art-injener/satellite-scout/internal/sdr"
)

func TestMeta_CoversAllDemods(t *testing.T) {
	for _, id := range sdr.Demods {
		if id == DemodAuto {
			continue // режим подбора, не модуляция
		}
		m, ok := Meta(id)
		if !ok {
			t.Errorf("missing meta for %q", id)
			continue
		}
		if m.ID != id {
			t.Errorf("%q: ID=%q", id, m.ID)
		}
		if m.Label == "" {
			t.Errorf("%q: empty Label", id)
		}
		if m.Family == "" {
			t.Errorf("%q: empty Family", id)
		}
		if m.DefaultBandwidthHz == 0 {
			t.Errorf("%q: DefaultBandwidthHz=0", id)
		}
		// Линейные профили эфира (fsk9600 линия) без SatNOGSModes — не в «Авто» по режиму.
		if len(m.SatNOGSModes) == 0 && !m.BaudFlexible {
			t.Errorf("%q: empty SatNOGSModes", id)
		}
	}
}

func TestMeta_Unknown(t *testing.T) {
	if _, ok := Meta("no_such_profile"); ok {
		t.Fatal("unknown id must miss")
	}
}

func TestCandidates_FSK9600(t *testing.T) {
	got := Candidates("FSK", 9600)
	want := []string{"fsk9600_g3ruh", "fsk9600_geoscan", "fsk9600_ax100_rs"}
	if !slices.Equal(got, want) {
		t.Fatalf("FSK 9600: got %v, want %v", got, want)
	}
	// GFSK: старые fsk* (SatNOGS пишет GFSK) плюс явные gfsk*.
	wantGFSK := []string{"fsk9600_g3ruh", "fsk9600_geoscan", "fsk9600_ax100_rs", "gfsk9600"}
	if !slices.Equal(Candidates("gfsk", 9600), wantGFSK) {
		t.Fatalf("GFSK 9600: %v, want %v", Candidates("gfsk", 9600), wantGFSK)
	}
}

func TestCandidates_FixedDigital(t *testing.T) {
	cases := []struct {
		mode string
		baud float64
		want []string
	}{
		{"AFSK", 1200, []string{"afsk1200"}},
		{"FSK", 1200, []string{"fsk1200"}},
		{"FSK", 4800, []string{"fsk4800", "fsk4800_ax100", "3fsk4800"}},
		{"GFSK", 1200, []string{"fsk1200", "gfsk1200"}},
		{"GFSK", 4800, []string{"fsk4800", "fsk4800_ax100", "gfsk4800", "rs41"}},
		{"GMSK", 9600, []string{"gmsk9600", "gmsk9600_ax100", "gmsk9600_ax100_rs", "gmsk9600_usp"}},
		{"GMSK", 4800, []string{"gmsk4800", "gmsk4800_ax100", "gmsk4800_usp"}},
		{"GMSK", 2400, []string{"gmsk2400", "gmsk2400_usp"}},
		{"MSK", 9600, []string{"msk9600"}},
		{"3FSK", 4800, []string{"3fsk4800"}},
		{"4FSK", 4800, []string{"4fsk4800", "gmfsk4800"}},
		{"DMR", 4800, []string{"4fsk4800"}},
		{"GMFSK", 4800, []string{"gmfsk4800"}},
		{"BPSK", 1200, []string{"bpsk1200"}},
		{"BPSK", 9600, []string{"bpsk9600"}},
		{"QPSK", 0, []string{"qpsk_ccsds"}},
		{"QPSK", 9600, []string{"qpsk_ccsds"}},
		{"OOK", 0, []string{"ook"}},
		{"OOK", 2000, []string{"ook"}},
		{"DQPSK", 0, []string{"pi4_dqpsk"}},
		{"PI4DQPSK", 9600, []string{"pi4_dqpsk"}},
		{"APSK", 0, []string{"apsk16"}},
		{"16APSK", 4800, []string{"apsk16"}},
	}
	for _, tc := range cases {
		got := Candidates(tc.mode, tc.baud)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s %.0f: got %v, want %v", tc.mode, tc.baud, got, tc.want)
		}
	}
}

func TestCandidates_VoiceAndCW(t *testing.T) {
	cases := []struct {
		mode string
		want []string
	}{
		{"AM", []string{"am"}},
		{"FM", []string{"fm"}},
		{"NFM", []string{"fm"}},
		{"WFM", []string{"wfm"}},
		{"CW", []string{"cw"}},
		{"USB", []string{"usb"}},
		{"LSB", []string{"lsb"}},
		{"SSB", []string{"usb", "lsb"}},
		{"SSTV", []string{"sstv"}},
	}
	for _, tc := range cases {
		got := Candidates(tc.mode, 0)
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.mode, got, tc.want)
		}
		if got := Candidates(tc.mode, 9600); len(got) != 0 {
			t.Errorf("%s with baud must be empty for analog: %v", tc.mode, got)
		}
	}
}

func TestCandidates_Empty(t *testing.T) {
	if got := Candidates("FSK", 0); len(got) != 0 {
		t.Errorf("FSK without baud: %v", got)
	}
	if got := Candidates("UNKNOWN", 1200); len(got) != 0 {
		t.Errorf("unknown mode: %v", got)
	}
	if got := Candidates("", 0); len(got) != 0 {
		t.Errorf("empty mode: %v", got)
	}
}

func TestListMeta_OrderMatchesDemods(t *testing.T) {
	list := ListMeta()
	var want []string
	for _, id := range sdr.Demods {
		if id != DemodAuto {
			want = append(want, id)
		}
	}
	if len(list) != len(want) {
		t.Fatalf("len=%d, want %d", len(list), len(want))
	}
	for i, id := range want {
		if list[i].ID != id {
			t.Fatalf("at %d: %q, want %q", i, list[i].ID, id)
		}
	}
}
