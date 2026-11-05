package sound

import "testing"

func TestFactoryComplete(t *testing.T) {
	f := Factory()
	if len(f) != 16 {
		t.Fatalf("got %d presets", len(f))
	}
	for _, p := range f {
		if len(p.P) != len(Defaults) {
			t.Errorf("%s: %d params", p.Name, len(p.P))
		}
	}
}

func TestSlotRoundTrip(t *testing.T) {
	d := t.TempDir()
	p := Factory()[1]
	p.Name = "MY WOB"
	if err := SaveFile(SlotPath(d, 13), p); err != nil {
		t.Fatal(err)
	}
	q, user := LoadSlot(d, 13)
	if !user || q.Name != "MY WOB" || q.P["wobble_depth"] != p.P["wobble_depth"] {
		t.Fatalf("%+v", q)
	}
}
