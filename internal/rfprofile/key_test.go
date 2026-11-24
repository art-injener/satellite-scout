package rfprofile

import "testing"

func TestKey_UUIDPreferred(t *testing.T) {
	got := Key("abc-uuid", 401_000_123)
	if got != "abc-uuid" {
		t.Fatalf("Key = %q, want UUID", got)
	}
}

func TestKey_NoUUID_QuantizesTo5kHz(t *testing.T) {
	cases := []struct {
		hz   int64
		want string
	}{
		{401_000_000, "401000000"},
		{401_002_499, "401000000"},
		{401_002_500, "401005000"},
		{400_997_500, "401000000"},
		{400_997_499, "400995000"},
	}
	for _, tc := range cases {
		if got := Key("", tc.hz); got != tc.want {
			t.Fatalf("Key(\"\", %d) = %q, want %q", tc.hz, got, tc.want)
		}
	}
}

func TestQuantizeHz_Step(t *testing.T) {
	if got := QuantizeHz(435_402_400); got != 435_400_000 {
		t.Fatalf("QuantizeHz = %d, want 435400000", got)
	}
}
