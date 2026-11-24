package ccsds

import (
	"bytes"
	"testing"

	"github.com/art-injener/satellite-scout/internal/modem"
)

// Модем QPSK → выделитель CADU без канальных фильтров: проверка стыка линии и кадра.
func TestQPSKModem_DeframerRoundTrip(t *testing.T) {
	const rate = 64_000
	want := []byte("qpsk modem cadu roundtrip")
	levels, err := Encode(want, 128, 4)
	if err != nil {
		t.Fatal(err)
	}
	cfg := modem.QPSKCCSDS(rate)
	iq := modem.NewQPSK(cfg).Modulate(nil, levels)
	bits := modem.NewQPSKDemod(cfg).Demodulate(nil, iq)
	var d Deframer
	got := d.Push(nil, bits)
	if len(got) == 0 {
		t.Fatalf("no frames good=%d bad=%d sync=%d bits=%d", d.Good(), d.Bad(), d.SyncWords(), len(bits))
	}
	if !bytes.HasPrefix(got[0], want) {
		t.Fatalf("prefix=%q want %q", got[0][:min(len(got[0]), len(want))], want)
	}
}
