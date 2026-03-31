package tracker

import "testing"

func TestParseSatName(t *testing.T) {
	tests := []struct {
		input         string
		wantPrimary   string
		wantAlias     string
	}{
		{"ISS (ZARYA)", "ISS", "ZARYA"},
		{"YUBILEINY (RS30)", "YUBILEINY", "RS30"},
		{"OOV-CUBE (TUBSAT-30)", "OOV-CUBE", "TUBSAT-30"},
		{"RS-44 & BREEZE-KM R/B", "RS-44", "BREEZE-KM R/B"},
		{"METEOR-M 2-3", "METEOR-M 2-3", ""},
		{"NOAA 18", "NOAA 18", ""},
		{"", "", ""},
		{"  ISS (ZARYA)  ", "ISS", "ZARYA"},
		{"FOO (", "FOO (", ""},
	}
	for _, tt := range tests {
		primary, alias := ParseSatName(tt.input)
		if primary != tt.wantPrimary || alias != tt.wantAlias {
			t.Errorf("ParseSatName(%q) = (%q, %q), want (%q, %q)",
				tt.input, primary, alias, tt.wantPrimary, tt.wantAlias)
		}
	}
}
