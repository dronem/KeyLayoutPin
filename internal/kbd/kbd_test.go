package kbd

import "testing"

func TestRoundTrip(t *testing.T) {
	for _, h := range []HKL{0x04110411, 0x04150409, 0} {
		got, ok := Parse(h.String())
		if !ok || got != h {
			t.Errorf("Parse(%s) = %v, %v", h, got, ok)
		}
	}
	if _, ok := Parse("garbage"); ok {
		t.Error("garbage must not parse")
	}
	h := HKL(0x04150409)
	if h.LangID() != 0x0409 || h.Variant() != 0x0415 {
		t.Errorf("LangID/Variant of %s = %04X/%04X", h, h.LangID(), h.Variant())
	}
}
