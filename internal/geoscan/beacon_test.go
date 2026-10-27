package geoscan

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/art-injener/satellite-scout/internal/ax25"
)

// testBeaconInfo — a beacon with values placed by hand at the offsets of the platform layout.
func testBeaconInfo() []byte {
	info := make([]byte, BeaconLen)
	info[0] = 1                                            // beacon id
	binary.LittleEndian.PutUint32(info[1:], 1_700_000_000) // board time
	info[5] = 3                                            // EPS mode: normal
	binary.LittleEndian.PutUint16(info[7:], 250)           // consumption current, mA
	binary.LittleEndian.PutUint16(info[9:], 180)           // solar cells current, mA
	binary.LittleEndian.PutUint16(info[13:], 7412)         // battery voltage, mV
	binary.LittleEndian.PutUint16(info[15:], 0x0007)       // systems status bits
	info[17] = 0xFB                                        // cell 1 temperature: -5 °C
	info[18] = 21                                          // cell 2 temperature
	info[31] = 7                                           // GNSS satellites
	info[32] = 2                                           // ADCS mode: three-axis
	binary.LittleEndian.PutUint16(info[41:], 5025)         // bus voltage, mV
	info[45] = 0x9C                                        // RSSI: -100 dBm
	info[51] = 1                                           // COMM mode: Mode III
	return info
}

func beaconFrame(info []byte) ax25.Frame {
	return ax25.Frame{Dest: ax25.Address{Call: BeaconDest}, Src: ax25.Address{Call: "RS23S"}, Info: info}
}

func paramsByID(params []Param) map[string]Param {
	m := make(map[string]Param, len(params))
	for _, p := range params {
		m[p.ID] = p
	}
	return m
}

func TestBeaconLayout_FillsBeacon(t *testing.T) {
	size := 0
	for _, f := range beaconLayout {
		size += f.size
	}
	if size != BeaconLen {
		t.Errorf("layout covers %d bytes, want %d", size, BeaconLen)
	}
}

func TestBeaconParams_DecodesFields(t *testing.T) {
	params, ok := BeaconParams(beaconFrame(testBeaconInfo()))
	if !ok {
		t.Fatal("beacon not recognized")
	}
	if len(params) != len(beaconLayout) {
		t.Fatalf("got %d params, want %d", len(params), len(beaconLayout))
	}
	got := paramsByID(params)
	for _, c := range []struct{ id, value, unit string }{
		{"beacon_id", "1", ""},
		{"eps_timestamp", "14.11.2023 22:13:20", "UTC"},
		{"eps_mode", "штатный", ""},
		{"eps_consumption_current", "250", "мА"},
		{"eps_solar_cells_current", "180", "мА"},
		{"eps_cell_voltage_full", "7,412", "В"},
		{"eps_systems_status", "0x0007", ""},
		{"eps_temperature_cell1", "-5", "°C"},
		{"eps_temperature_cell2", "21", "°C"},
		{"gnss_sat_number", "7", ""},
		{"adcs_mode", "трёхосная", ""},
		{"comm_bus_voltage", "5,025", "В"},
		{"comm_rssi", "-100", "дБм"},
		{"comm_mode", "III", ""},
		{"comm_reserved", "0x0000", ""},
	} {
		p, found := got[c.id]
		if !found {
			t.Errorf("%s: missing", c.id)
			continue
		}
		if p.Value != c.value || p.Unit != c.unit {
			t.Errorf("%s=%q %q, want %q %q", c.id, p.Value, p.Unit, c.value, c.unit)
		}
		if p.Name == "" {
			t.Errorf("%s: no display name", c.id)
		}
	}
}

func TestBeaconParams_UnknownEnumShownAsNumber(t *testing.T) {
	info := testBeaconInfo()
	info[5] = 9
	params, _ := BeaconParams(beaconFrame(info))
	if got := paramsByID(params)["eps_mode"].Value; got != "9" {
		t.Errorf("eps_mode=%q, want 9", got)
	}
}

func TestBeaconParams_KeyFieldsForTableRow(t *testing.T) {
	params, _ := BeaconParams(beaconFrame(testBeaconInfo()))
	var key []string
	for _, p := range params {
		if p.Key {
			key = append(key, p.ID)
		}
	}
	want := []string{
		"eps_timestamp", "eps_mode", "eps_consumption_current", "eps_solar_cells_current",
		"eps_cell_voltage_full", "eps_temperature_cell1", "eps_temperature_cell2", "gnss_sat_number", "comm_rssi",
	}
	if len(key) != len(want) {
		t.Fatalf("key fields %v, want %v", key, want)
	}
	for i := range want {
		if key[i] != want[i] {
			t.Errorf("key fields %v, want %v", key, want)
			break
		}
	}
}

func TestBeaconParams_RejectsOtherFrames(t *testing.T) {
	for name, f := range map[string]ax25.Frame{
		"short info": beaconFrame(testBeaconInfo()[:BeaconLen-1]),
		"long info":  beaconFrame(append(testBeaconInfo(), 0)),
		"other dest": {Dest: ax25.Address{Call: "CQ"}, Src: ax25.Address{Call: "RS23S"}, Info: testBeaconInfo()},
	} {
		if _, ok := BeaconParams(f); ok {
			t.Errorf("%s: recognized as beacon", name)
		}
	}
}

func TestBeaconParams_AcceptsRSDest(t *testing.T) {
	f := beaconFrame(testBeaconInfo())
	f.Dest = ax25.Address{Call: "RS20S"}
	if _, ok := BeaconParams(f); !ok {
		t.Error("beacon to RS… not recognized")
	}
}

func TestBuildBeacon_RoundTrip(t *testing.T) {
	info, err := BuildBeacon(map[string]float64{
		"beacon_id":             1,
		"eps_timestamp":         1_700_000_000,
		"eps_mode":              3,
		"eps_cell_voltage_full": 7.412,
		"eps_temperature_cell1": -5,
		"comm_rssi":             -100,
		"comm_bus_voltage":      5.025,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, BeaconLen)
	want[0] = 1
	binary.LittleEndian.PutUint32(want[1:], 1_700_000_000)
	want[5] = 3
	binary.LittleEndian.PutUint16(want[13:], 7412)
	want[17] = 0xFB
	binary.LittleEndian.PutUint16(want[41:], 5025)
	want[45] = 0x9C
	if string(info) != string(want) {
		t.Errorf("beacon % X\nwant   % X", info, want)
	}
}

func TestBuildBeacon_UnknownField(t *testing.T) {
	if _, err := BuildBeacon(map[string]float64{"no_such_field": 1}); !errors.Is(err, ErrUnknownField) {
		t.Errorf("err=%v, want ErrUnknownField", err)
	}
}

func TestBuildBeacon_OutOfRange(t *testing.T) {
	for id, v := range map[string]float64{"gnss_sat_number": 256, "comm_rssi": -129, "beacon_id": -1} {
		if _, err := BuildBeacon(map[string]float64{id: v}); !errors.Is(err, ErrOutOfRange) {
			t.Errorf("%s=%v: err=%v, want ErrOutOfRange", id, v, err)
		}
	}
}
