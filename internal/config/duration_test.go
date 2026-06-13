package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    time.Duration
		wantErr bool
	}{
		{"6h", `"6h"`, 6 * time.Hour, false},
		{"24h", `"24h"`, 24 * time.Hour, false},
		{"12s", `"12s"`, 12 * time.Second, false},
		{"1h30m", `"1h30m"`, 90 * time.Minute, false},
		{"number rejected", `21600000000000`, 0, true},
		{"empty string", `""`, 0, true},
		{"invalid unit", `"2 min"`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal([]byte(tt.json), &d)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if d.Duration() != tt.want {
				t.Errorf("got %v, want %v", d.Duration(), tt.want)
			}
		})
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		d    Duration
		want string
	}{
		{Duration(6 * time.Hour), `"6h"`},
		{Duration(24 * time.Hour), `"24h"`},
		{Duration(12 * time.Second), `"12s"`},
		{Duration(90 * time.Minute), `"1h30m"`},
		{Duration(5 * time.Minute), `"5m"`},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := json.Marshal(tt.d)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestConfig_DurationFieldsJSONRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal raw: %v", err)
	}
	tle, ok := raw["tle"].(map[string]any)
	if !ok {
		t.Fatal("tle section missing")
	}
	if tle["update_interval"] != "6h" {
		t.Errorf("tle.update_interval = %v, want %q", tle["update_interval"], "6h")
	}
	satnogs, ok := raw["satnogs"].(map[string]any)
	if !ok {
		t.Fatal("satnogs section missing")
	}
	if satnogs["cache_ttl"] != "24h" {
		t.Errorf("satnogs.cache_ttl = %v, want %q", satnogs["cache_ttl"], "24h")
	}
	if satnogs["timeout"] != "12s" {
		t.Errorf("satnogs.timeout = %v, want %q", satnogs["timeout"], "12s")
	}
}
