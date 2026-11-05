package seq

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultProject(t *testing.T) {
	p := Default()
	if p.Pat[0].Len != 32 || p.Pat[2].Len != 16 || p.Pat[5].Len != 16 {
		t.Fatalf("lens %d %d %d", p.Pat[0].Len, p.Pat[2].Len, p.Pat[5].Len)
	}
	if p.Tracks[0].Sound.Name != "BOOM KICK" || p.Tracks[4].Sound.P["voices"] != 1 {
		t.Fatalf("tracks %+v", p.Tracks[0])
	}
	if !p.Pat[0].S[0][0].On || p.Pat[0].S[0][0].Note != 36 {
		t.Fatal("kick on step 1 missing")
	}
	if v, ok := p.Pat[1].S[1][24].LockValue("delay_send"); !ok || v != 0.85 {
		t.Fatal("dub throw lock missing")
	}
	if len(p.Chain) != 8 || p.G["tempo"] != 94 {
		t.Fatalf("chain %v g %v", p.Chain, p.G)
	}
}

func TestRoundTrip(t *testing.T) {
	p := Default()
	p.Pat[3].Len = 7
	p.Pat[3].S[6][31] = Step{On: true, Note: 70, Vel: 0.5, Gate: 2, Micro: -0.2}
	p.Pat[3].S[6][31].SetLock("cutoff", 0.3)
	p.Tracks[2].Muted = true
	path := filepath.Join(t.TempDir(), "p.json")
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	q, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(q)
	if string(a) != string(b) {
		t.Fatal("round trip changed the project")
	}
}

func TestLocksMirrorEngine(t *testing.T) {
	var s Step
	for i, k := range []string{"cutoff", "timbre", "morph", "decay"} {
		if slot := s.SetLock(k, 0.1); slot != i {
			t.Fatalf("slot %d for %s", slot, k)
		}
	}
	if s.SetLock("timbre", 0.9) != 1 || s.Locks[1].V != 0.9 {
		t.Fatal("same key must replace")
	}
	if s.SetLock("drive", 0.5) != 3 || s.Locks[3].Key != "drive" || len(s.Locks) != 4 {
		t.Fatal("full: last slot is overwritten")
	}
}

func TestCommandsAndReplies(t *testing.T) {
	p := Default()
	cmds := p.Commands()
	joined := strings.Join(cmds, "\n")
	for _, want := range []string{"STOP", "P tempo 94", "T 0 engine 21", "LEN 0 32", "S 0 0 0 1 36 1 0.5 0", "L 1 1 24 delay_send 0.85", "CHAIN 0 0 1 1 2 2 2 2", "PAT 0", "U 0"} {
		if !strings.Contains(joined, want+"\n") && !strings.HasSuffix(joined, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, c := range cmds {
		if strings.HasPrefix(c, "T ") && strings.Contains(c, " tempo ") {
			t.Fatal("tempo must be global, not a track param")
		}
	}
	if _, _, _, ok := p.ApplyReply("REC 2 7 5 72 0.700 0.100"); !ok {
		t.Fatal("REC not applied")
	}
	st := p.Pat[2].S[7][5]
	if !st.On || st.Note != 72 || st.Vel != 0.7 || st.Micro != 0.1 {
		t.Fatalf("%+v", st)
	}
	p.ApplyReply("RECG 2 7 5 1.999")
	p.ApplyReply("RECL 2 7 6 cutoff 0.4500")
	if p.Pat[2].S[7][5].Gate != 1.999 {
		t.Fatal("gate")
	}
	if v, ok := p.Pat[2].S[7][6].LockValue("cutoff"); !ok || v != 0.45 {
		t.Fatal("lock")
	}
	if _, _, _, ok := p.ApplyReply("REC 99 0 0 1 1 0"); ok {
		t.Fatal("out of range accepted")
	}
}
