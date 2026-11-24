package ax100

import (
	"testing"
)

func TestSuchaiBeaconParams(t *testing.T) {
	raw := PackSuchaiBeacon(SuchaiPort2, map[string]uint32{
		"eps_vbatt":         7400,
		"eps_cur_sun":       120,
		"obc_hrs_alive":     213,
		"obc_reset_counter": 15,
		"obc_temp_1":        28,
		"eps_temp_bat0":     19,
		"timestamp":         1_718_553_388,
	})
	params, sat, ok := SuchaiBeaconParams(raw)
	if !ok {
		t.Fatal("expected suchai beacon")
	}
	if sat != "SUCHAI-2" {
		t.Fatalf("sat=%q", sat)
	}
	byID := map[string]string{}
	for _, p := range params {
		byID[p.ID] = p.Value
	}
	if byID["eps_vbatt"] != "7,40" {
		t.Fatalf("vbatt=%q", byID["eps_vbatt"])
	}
	if byID["obc_hrs_alive"] != "213" || byID["obc_reset_counter"] != "15" {
		t.Fatalf("params=%v", byID)
	}
	if byID["sat"] != "SUCHAI-2" {
		t.Fatalf("sat param=%q", byID["sat"])
	}
}

func TestSuchaiBeaconParams_RejectsOtherPort(t *testing.T) {
	raw := PackCSP(CSPHeader{
		Priority: 2, Source: 1, Destination: 10, SrcPort: 14, DstPort: 20, CRC: true,
	}, make([]byte, suchaiMinBody))
	if _, _, ok := SuchaiBeaconParams(raw); ok {
		t.Fatal("dst_port 20 must not match suchai2 layout")
	}
}

func TestSuchaiBeaconParams_PlantSat(t *testing.T) {
	raw := PackSuchaiBeacon(SuchaiPortPlant, map[string]uint32{"eps_vbatt": 7100})
	_, sat, ok := SuchaiBeaconParams(raw)
	if !ok || sat != "PlantSat" {
		t.Fatalf("ok=%v sat=%q", ok, sat)
	}
}
