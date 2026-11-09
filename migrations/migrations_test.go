package migrations

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestAllEmbedded(t *testing.T) {
	ms, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) < 2 {
		t.Fatalf("want >= 2 migrations, got %d", len(ms))
	}
	if !strings.Contains(ms[0].SQL, "entries_no_update") {
		t.Fatal("0001 must define append-only triggers")
	}
}

func TestParse(t *testing.T) {
	sql := &fstest.MapFile{Data: []byte("SELECT 1;")}
	cases := []struct {
		name    string
		files   fstest.MapFS
		want    []int
		wantErr bool
	}{
		{"ordered", fstest.MapFS{"0002_b.sql": sql, "0001_a.sql": sql}, []int{1, 2}, false},
		{"gap", fstest.MapFS{"0001_a.sql": sql, "0003_c.sql": sql}, nil, true},
		{"duplicate", fstest.MapFS{"0001_a.sql": sql, "1_b.sql": sql}, nil, true},
		{"no underscore", fstest.MapFS{"0001.sql": sql}, nil, true},
		{"bad prefix", fstest.MapFS{"abc_x.sql": sql}, nil, true},
		{"empty", fstest.MapFS{}, []int{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, err := parse(tc.files)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(ms) != len(tc.want) {
				t.Fatalf("got %d migrations, want %d", len(ms), len(tc.want))
			}
			for i, v := range tc.want {
				if ms[i].Version != v {
					t.Fatalf("ms[%d].Version = %d, want %d", i, ms[i].Version, v)
				}
			}
		})
	}
}
