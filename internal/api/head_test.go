package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaxzTan/kira/internal/ledger"
)

func TestParseHead(t *testing.T) {
	h64 := strings.Repeat("a", 64)
	tests := []struct {
		in   string
		ok   bool
		want ledger.Head
	}{
		{"12:" + h64 + "\n", true, ledger.Head{ID: 12, Hash: h64}},
		{"0:" + ledger.GenesisHash, true, ledger.Head{ID: 0, Hash: ledger.GenesisHash}},
		{"12" + h64, false, ledger.Head{}},
		{"x:" + h64, false, ledger.Head{}},
		{"-1:" + h64, false, ledger.Head{}},
		{"1:" + strings.ToUpper(h64), false, ledger.Head{}},
		{"1:abc", false, ledger.Head{}},
		{"", false, ledger.Head{}},
	}
	for _, tt := range tests {
		got, err := ParseHead(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("ParseHead(%q) = %+v, %v", tt.in, got, err)
		}
		if tt.ok && FormatHead(got)+"\n" != tt.in && FormatHead(got) != tt.in {
			t.Errorf("FormatHead round trip %q", tt.in)
		}
	}
}

func TestHeadFileAdvance(t *testing.T) {
	dir := t.TempDir()
	hf := NewHeadFile(filepath.Join(dir, "head"))
	if hd, err := hf.Read(); hd != nil || err != nil {
		t.Fatalf("missing file = %+v %v", hd, err)
	}
	a, b, c := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	steps := []struct {
		name  string
		row   ledger.Row
		wrote bool
		want  int64
	}{
		{"first row from genesis", ledger.Row{ID: 1, PrevHash: ledger.GenesisHash, Hash: a}, true, 1},
		{"contiguous", ledger.Row{ID: 2, PrevHash: a, Hash: b}, true, 2},
		{"stale (older row)", ledger.Row{ID: 2, PrevHash: a, Hash: b}, false, 2},
		{"gap", ledger.Row{ID: 4, PrevHash: c, Hash: c}, false, 2},
		{"prev mismatch", ledger.Row{ID: 3, PrevHash: c, Hash: c}, false, 2},
		{"contiguous again", ledger.Row{ID: 3, PrevHash: b, Hash: c}, true, 3},
	}
	for _, s := range steps {
		wrote, err := hf.Advance(s.row)
		if err != nil || wrote != s.wrote {
			t.Fatalf("%s: wrote=%v err=%v", s.name, wrote, err)
		}
		hd, _ := hf.Read()
		if hd.ID != s.want {
			t.Fatalf("%s: head %d want %d", s.name, hd.ID, s.want)
		}
	}
	fi, _ := os.Stat(hf.Path())
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("temp files left behind: %v", ents)
	}
	// Advance with no file and a non-genesis row does nothing.
	hf2 := NewHeadFile(filepath.Join(dir, "head2"))
	if wrote, _ := hf2.Advance(ledger.Row{ID: 5, PrevHash: a, Hash: b}); wrote {
		t.Fatal("advanced from nothing")
	}
}
