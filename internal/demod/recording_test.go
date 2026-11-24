package demod

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/art-injener/satellite-scout/internal/payload"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sigmf"
)

// Манифест регрессии на записях (ADR-009 §10.2). Записи в git не входят — нет файла → skip.
type recordingManifest struct {
	Entries []recordingEntry `json:"entries"`
}

type recordingEntry struct {
	ID           string          `json:"id"`
	Meta         string          `json:"meta"`
	Profile      string          `json:"profile"`
	OffsetHz     int64           `json:"offset_hz"`
	BandwidthHz  uint32          `json:"bandwidth_hz"`
	SquelchDBFS  float64         `json:"squelch_dbfs"`
	Expect       recordingExpect `json:"expect"`
}

type recordingExpect struct {
	CRCGoodMin int               `json:"crc_good_min"`
	Kinds      recordingKindExp  `json:"kinds"`
}

type recordingKindExp struct {
	ImageMin   int `json:"image_min"`
	BeaconMin  int `json:"beacon_min"`
	UnknownMax int `json:"unknown_max"`
}

func TestRecordingManifest_PayloadKinds(t *testing.T) {
	root := filepath.Join("..", "..", "data", "recordings")
	manPath := filepath.Join(root, "manifest.json")
	raw, err := os.ReadFile(manPath)
	if err != nil {
		t.Skipf("no manifest: %v", err)
	}
	var man recordingManifest
	if err := json.Unmarshal(raw, &man); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(man.Entries) == 0 {
		t.Fatal("manifest has no entries")
	}
	for _, e := range man.Entries {
		t.Run(e.ID, func(t *testing.T) {
			runRecordingEntry(t, root, e)
		})
	}
}

func runRecordingEntry(t *testing.T, root string, e recordingEntry) {
	t.Helper()
	meta := filepath.Join(root, e.Meta)
	ds, err := sigmf.Open(meta)
	if err != nil {
		t.Skipf("no recording %s: %v", e.Meta, err)
	}
	defer ds.Close()

	bw := e.BandwidthHz
	if bw == 0 {
		bw = 20_000
	}
	sq := e.SquelchDBFS
	if sq == 0 {
		sq = -90
	}
	cs := sdr.ChannelSettings{
		OffsetHz: e.OffsetHz, BandwidthHz: bw, SquelchDBFS: sq, Demod: e.Profile,
	}

	c := NewChain()
	block := sdr.DefaultBlockSize
	buf := make([]byte, block*8)
	iq := make([]complex64, block)
	kinds := map[string]int{}
	var nPkts int
	var ts uint64
	rate := ds.SampleRate()
	center := ds.CenterHz()
	for {
		n, rerr := io.ReadFull(ds, buf)
		if n < 8 {
			break
		}
		nSamples := n / 8
		for i := 0; i < nSamples; i++ {
			re := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8:]))
			im := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*8+4:]))
			iq[i] = complex(re, im)
		}
		pkts := c.Process(sdr.IQFrame{
			Samples: iq[:nSamples], SampleRate: rate, CenterHz: center, TimestampMs: ts,
		}, cs)
		ts += uint64(nSamples) * 1000 / uint64(rate)
		for _, p := range pkts {
			nPkts++
			pl := payload.Decode(payload.Frame{
				Raw: p.Raw, Profile: e.Profile, FrequencyHz: p.FrequencyHz,
			})
			kinds[pl.Kind]++
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			t.Fatal(rerr)
		}
	}

	st := c.Status()
	t.Logf("packets=%d status.good=%d bad=%d kinds=%v", nPkts, st.Good, st.Bad, kinds)
	if st.Good < e.Expect.CRCGoodMin {
		t.Errorf("crc good=%d, want >= %d", st.Good, e.Expect.CRCGoodMin)
	}
	if kinds[payload.KindImage] < e.Expect.Kinds.ImageMin {
		t.Errorf("image=%d, want >= %d", kinds[payload.KindImage], e.Expect.Kinds.ImageMin)
	}
	if kinds[payload.KindBeacon] < e.Expect.Kinds.BeaconMin {
		t.Errorf("beacon=%d, want >= %d", kinds[payload.KindBeacon], e.Expect.Kinds.BeaconMin)
	}
	if kinds[payload.KindUnknown] > e.Expect.Kinds.UnknownMax {
		t.Errorf("unknown=%d, want <= %d", kinds[payload.KindUnknown], e.Expect.Kinds.UnknownMax)
	}
}
