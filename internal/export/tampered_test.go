package export

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
)

func TestWriteZipTamperedReceipts(t *testing.T) {
	cases := []struct {
		name         string
		tamper       func(t *testing.T, dir string, a, b receipts.Info)
		wantFiles    []string // besides expenses.csv and receipts/*
		wantTampered []string
	}{
		{"intact", nil, nil, nil},
		{"one overwritten", func(t *testing.T, dir string, a, _ receipts.Info) {
			if err := os.WriteFile(filepath.Join(dir, a.FileName()), []byte("%PDF-1.4 forged"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, []string{"TAMPERED_RECEIPTS.txt"}, []string{"A"}},
		{"one overwritten, one deleted", func(t *testing.T, dir string, a, b receipts.Info) {
			os.WriteFile(filepath.Join(dir, b.FileName()), []byte("x"), 0o600)
			os.Remove(filepath.Join(dir, a.FileName()))
		}, []string{"MISSING_RECEIPTS.txt", "TAMPERED_RECEIPTS.txt"}, []string{"B"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, err := receipts.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a, _ := rs.Save(bytes.NewReader([]byte("%PDF-1.4\n%A\n%%EOF\n")), -1)
			b, _ := rs.Save(bytes.NewReader([]byte("%PDF-1.4\n%B\n%%EOF\n")), -1)
			if tc.tamper != nil {
				tc.tamper(t, rs.Dir(), a, b)
			}
			rows := []ledger.Entry{
				{ID: 1, OccurredOn: "2026-11-01", AmountSen: 1, Payee: "a", Method: ledger.MethodCard, Status: ledger.StatusActive, ReceiptSHA256: &a.SHA256},
				{ID: 2, OccurredOn: "2026-11-02", AmountSen: 2, Payee: "b", Method: ledger.MethodCard, Status: ledger.StatusActive, ReceiptSHA256: &b.SHA256},
			}
			var buf bytes.Buffer
			if err := WriteZip(&buf, rows, rs); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			for _, f := range zr.File {
				rc, _ := f.Open()
				d, _ := io.ReadAll(rc)
				rc.Close()
				got[f.Name] = string(d)
			}
			for _, n := range tc.wantFiles {
				if _, ok := got[n]; !ok {
					t.Fatalf("missing %s in %v", n, got)
				}
			}
			tl, has := got["TAMPERED_RECEIPTS.txt"]
			if has != (len(tc.wantTampered) > 0) {
				t.Fatalf("TAMPERED_RECEIPTS.txt present=%v", has)
			}
			names := map[string]receipts.Info{"A": a, "B": b}
			for _, w := range tc.wantTampered {
				fn := names[w].FileName()
				if !strings.Contains(tl, fn) {
					t.Fatalf("tampered list %q lacks %s", tl, fn)
				}
				if _, ok := got["receipts/"+fn]; !ok {
					t.Fatal("tampered file must still be included as evidence")
				}
			}
			for k, info := range names {
				if strings.Contains(tl, info.FileName()) && !contains(tc.wantTampered, k) {
					t.Fatalf("%s wrongly listed as tampered", k)
				}
			}
		})
	}
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
