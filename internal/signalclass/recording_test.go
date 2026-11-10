package signalclass

import (
	"encoding/binary"
	"os"
	"testing"
)

// GEOSCAN-Edelveis pass recorded by PE0SAT (data/recordings/SOURCES.txt), ci16_le at 57.6 kS/s.
const geoscanRecording = "../../data/recordings/pe0sat-geoscan/geoscan_edelveis_2022-11-24_57600.sigmf-data"

// readCI16 reads n complex samples from sample offset start of a ci16_le file.
func readCI16(t *testing.T, path string, start, n int) []complex64 {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("no recording: %v", err)
	}
	defer f.Close()
	raw := make([]byte, 4*n)
	if _, err = f.ReadAt(raw, int64(4*start)); err != nil {
		t.Fatal(err)
	}
	out := make([]complex64, n)
	for i := range out {
		re := int16(binary.LittleEndian.Uint16(raw[4*i:]))
		im := int16(binary.LittleEndian.Uint16(raw[4*i+2:]))
		out[i] = complex(float32(re)/32768, float32(im)/32768)
	}
	return out
}

// Slices the detector hands over for GEOSCAN packets: 2FSK 9600 with deviation about ±4 kHz.
// Single packets padded for the detector delay and a run of packets with short pauses.
func TestClassify_GeoscanPacketsAre2FSK(t *testing.T) {
	const rate = 57_600
	cases := []struct {
		name           string
		start, n       int
		offsetHz, bwHz float64
	}{
		{"packet in 400 ms", 5_858_048, 23_040, -18, 14_850},
		{"strong packet in 400 ms", 7_738_150, 23_040, 418, 15_975},
		{"packet in 614 ms", 9_605_888, 35_366, 1856, 14_625},
		{"packets with pauses", 10_362_752, 28_800, 1693, 15_300},
		// Weak transmissions: SNR in band about 8 dB, the bits still read.
		{"weak packets 213 s", 12_267_392, 28_800, 1265, 11_700},
		{"weak packets 226 s", 13_004_672, 28_800, 1020, 13_500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slice, r := Extract(readCI16(t, geoscanRecording, c.start, c.n), rate, c.offsetHz, c.bwHz)
			res := Classify(slice, r)
			t.Log(res.Why)
			if res.Modulation != Mod2FSK || res.Params.Baud != 9600 {
				t.Errorf("modulation %q at %.0f Bd, want %q at 9600 (why: %s)",
					res.Modulation, res.Params.Baud, Mod2FSK, res.Why)
			}
		})
	}
}
