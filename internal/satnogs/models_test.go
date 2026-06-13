package satnogs

import (
	"testing"
)

// ptrInt64 — короткий хелпер для nullable int64 в литералах.
func ptrInt64(v int64) *int64 { return &v }

// ptrFloat64 — короткий хелпер для nullable float64 в литералах.
func ptrFloat64(v float64) *float64 { return &v }

func TestTransmitter_IsActive(t *testing.T) {
	tests := []struct {
		name string
		tx   Transmitter
		want bool
	}{
		{"alive+active", Transmitter{Alive: true, Status: "active"}, true},
		{"alive+ACTIVE-case-insensitive", Transmitter{Alive: true, Status: "ACTIVE"}, true},
		{"dead+active", Transmitter{Alive: false, Status: "active"}, false},
		{"alive+inactive", Transmitter{Alive: true, Status: "inactive"}, false},
		{"alive+invalid", Transmitter{Alive: true, Status: "invalid"}, false},
		{"empty status", Transmitter{Alive: true, Status: ""}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tx.IsActive(); got != tt.want {
				t.Errorf("IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransmitter_HasDownlink(t *testing.T) {
	tests := []struct {
		name string
		tx   Transmitter
		want bool
	}{
		{"valid downlink", Transmitter{DownlinkLow: ptrInt64(145800000)}, true},
		{"nil downlink", Transmitter{DownlinkLow: nil}, false},
		{"zero downlink", Transmitter{DownlinkLow: ptrInt64(0)}, false},
		{"negative downlink", Transmitter{DownlinkLow: ptrInt64(-1)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tx.HasDownlink(); got != tt.want {
				t.Errorf("HasDownlink() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTransmitter_IsAmateurDownlink(t *testing.T) {
	tests := []struct {
		name   string
		freqHz int64
		want   bool
	}{
		{"VHF lower bound", 144_000_000, true},
		{"VHF middle (ISS APRS)", 145_825_000, true},
		{"VHF upper bound", 148_000_000, true},
		{"UHF lower bound", 430_000_000, true},
		{"UHF middle", 437_500_000, true},
		{"UHF upper bound", 440_000_000, true},
		{"below VHF (143 МГц)", 143_999_999, false},
		{"above VHF (149 МГц)", 149_000_000, false},
		{"L-band (1.2 ГГц)", 1_270_000_000, false},
		{"S-band (2.4 ГГц)", 2_400_500_000, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := Transmitter{DownlinkLow: ptrInt64(tt.freqHz)}
			if got := tx.IsAmateurDownlink(); got != tt.want {
				t.Errorf("IsAmateurDownlink(%d) = %v, want %v", tt.freqHz, got, tt.want)
			}
		})
	}

	// Без downlink — всегда false.
	t.Run("nil downlink", func(t *testing.T) {
		tx := Transmitter{DownlinkLow: nil}
		if tx.IsAmateurDownlink() {
			t.Error("IsAmateurDownlink() = true, want false (no downlink)")
		}
	})
}

func TestSelectPrimary_EmptyList(t *testing.T) {
	if got := SelectPrimary(nil); got != nil {
		t.Errorf("SelectPrimary(nil) = %v, want nil", got)
	}
	if got := SelectPrimary([]Transmitter{}); got != nil {
		t.Errorf("SelectPrimary([]) = %v, want nil", got)
	}
}

func TestSelectPrimary_FilterInactive(t *testing.T) {
	transmitters := []Transmitter{
		{UUID: "dead", Alive: false, Status: "active", DownlinkLow: ptrInt64(145800000), Mode: "FM"},
		{UUID: "inactive", Alive: true, Status: "inactive", DownlinkLow: ptrInt64(145800000), Mode: "FM"},
		{UUID: "no-downlink", Alive: true, Status: "active", DownlinkLow: nil, Mode: "FM"},
	}
	if got := SelectPrimary(transmitters); got != nil {
		t.Errorf("SelectPrimary() = %v, want nil (all filtered)", got)
	}
}

func TestSelectPrimary_PrefersAmateurOverHighBand(t *testing.T) {
	transmitters := []Transmitter{
		{UUID: "s-band", Alive: true, Status: "active", DownlinkLow: ptrInt64(2_400_500_000), Mode: "BPSK"},
		{UUID: "vhf-amateur", Alive: true, Status: "active", DownlinkLow: ptrInt64(145_825_000), Mode: "FM"},
	}
	primary := SelectPrimary(transmitters)
	if primary == nil {
		t.Fatal("SelectPrimary() = nil, want vhf-amateur")
	}
	if primary.UUID != "vhf-amateur" {
		t.Errorf("UUID = %q, want %q", primary.UUID, "vhf-amateur")
	}
}

func TestSelectPrimary_PrefersWithBaud(t *testing.T) {
	transmitters := []Transmitter{
		{UUID: "no-baud", Alive: true, Status: "active", DownlinkLow: ptrInt64(145_800_000), Mode: "FM"},
		{
			UUID:        "with-baud",
			Alive:       true,
			Status:      "active",
			DownlinkLow: ptrInt64(145_825_000),
			Mode:        "AFSK",
			Baud:        ptrFloat64(1200),
		},
	}
	primary := SelectPrimary(transmitters)
	if primary == nil {
		t.Fatal("SelectPrimary() = nil")
	}
	// Оба в любительском диапазоне → выбираем тот, у кого есть baud.
	if primary.UUID != "with-baud" {
		t.Errorf("UUID = %q, want %q", primary.UUID, "with-baud")
	}
}

func TestSelectPrimary_LowerFrequencyTiebreaker(t *testing.T) {
	transmitters := []Transmitter{
		{UUID: "higher", Alive: true, Status: "active", DownlinkLow: ptrInt64(437_500_000), Mode: "FM"},
		{UUID: "lower", Alive: true, Status: "active", DownlinkLow: ptrInt64(435_000_000), Mode: "FM"},
	}
	primary := SelectPrimary(transmitters)
	if primary == nil {
		t.Fatal("SelectPrimary() = nil")
	}
	// Оба в UHF amateur, оба без baud → tiebreaker = меньшая частота.
	if primary.UUID != "lower" {
		t.Errorf("UUID = %q, want %q", primary.UUID, "lower")
	}
}

func TestSelectPrimary_FormatsSummary(t *testing.T) {
	transmitters := []Transmitter{
		{
			UUID:        "iss-aprs",
			Description: "Mode V APRS",
			Alive:       true,
			Status:      "active",
			Type:        "Transceiver",
			DownlinkLow: ptrInt64(145_825_000),
			Mode:        "AFSK",
			Baud:        ptrFloat64(1200),
		},
	}
	primary := SelectPrimary(transmitters)
	if primary == nil {
		t.Fatal("SelectPrimary() = nil")
	}
	if primary.FreqHz != 145_825_000 {
		t.Errorf("FreqHz = %d, want 145825000", primary.FreqHz)
	}
	if primary.FreqMHz != "145.825" {
		t.Errorf("FreqMHz = %q, want %q", primary.FreqMHz, "145.825")
	}
	if primary.Mode != "AFSK" {
		t.Errorf("Mode = %q, want %q", primary.Mode, "AFSK")
	}
	if primary.Modulation != "AFSK 1200" {
		t.Errorf("Modulation = %q, want %q", primary.Modulation, "AFSK 1200")
	}
	if primary.Baud != 1200 {
		t.Errorf("Baud = %v, want 1200", primary.Baud)
	}
	if primary.Type != "Transceiver" {
		t.Errorf("Type = %q, want Transceiver", primary.Type)
	}
}

func TestSelectPrimary_NoBaudInModulation(t *testing.T) {
	transmitters := []Transmitter{
		{
			UUID: "fm-no-baud", Alive: true, Status: "active",
			DownlinkLow: ptrInt64(145_800_000), Mode: "FM",
		},
	}
	primary := SelectPrimary(transmitters)
	if primary == nil {
		t.Fatal("SelectPrimary() = nil")
	}
	if primary.Modulation != "FM" {
		t.Errorf("Modulation = %q, want %q", primary.Modulation, "FM")
	}
}

func TestFormatMHz(t *testing.T) {
	tests := []struct {
		hz   int64
		want string
	}{
		{145_825_000, "145.825"},
		{437_800_000, "437.800"},
		{2_400_500_000, "2400.500"},
		{0, ""},
		{-1, ""},
		{1000, "0.001"},
	}
	for _, tt := range tests {
		if got := formatMHz(tt.hz); got != tt.want {
			t.Errorf("formatMHz(%d) = %q, want %q", tt.hz, got, tt.want)
		}
	}
}

func TestFormatBaud(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{1200, "1200"},
		{9600, "9600"},
		{9.6, "9.6"},
		{50, "50"},
	}
	for _, tt := range tests {
		if got := formatBaud(tt.in); got != tt.want {
			t.Errorf("formatBaud(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
