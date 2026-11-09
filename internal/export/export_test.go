package export

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
)

func TestFormatSen(t *testing.T) {
	cases := []struct {
		sen  int64
		want string
	}{
		{0, "0.00"}, {1, "0.01"}, {10, "0.10"}, {99, "0.99"}, {100, "1.00"}, {4590, "45.90"},
		{123456, "1234.56"}, {999_999_999, "9999999.99"}, {-1, "-0.01"}, {-123456, "-1234.56"},
		{math.MaxInt64, "92233720368547758.07"}, {math.MinInt64, "-92233720368547758.08"},
	}
	for _, tc := range cases {
		if got := FormatSen(tc.sen); got != tc.want {
			t.Errorf("FormatSen(%d) = %s, want %s", tc.sen, got, tc.want)
		}
	}
}

func TestGuard(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""}, {"Cloudflare", "Cloudflare"}, {"=HYPERLINK(\"x\")", "'=HYPERLINK(\"x\")"},
		{"+60123", "'+60123"}, {"-5", "'-5"}, {"@SUM(A1)", "'@SUM(A1)"}, {"\tx", "'\tx"}, {"\rx", "'\rx"},
		{"a=b", "a=b"}, {"'quoted", "'quoted"},
	}
	for _, tc := range cases {
		if got := guard(tc.in); got != tc.want {
			t.Errorf("guard(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

type fakeReceipts map[string]string // sha -> path ("" = missing)

func (f fakeReceipts) Path(sha string) (string, receipts.Info, error) {
	p, ok := f[sha]
	if !ok || p == "" {
		return "", receipts.Info{}, receipts.ErrNotFound
	}
	ext := p[strings.LastIndex(p, ".")+1:]
	return p, receipts.Info{SHA256: sha, Ext: ext}, nil
}

func sp(s string) *string { return &s }

func sampleRows(shaPDF, shaMissing string) []ledger.Entry {
	return []ledger.Entry{
		{ID: 1, Direction: ledger.DirectionExpense, OccurredOn: "2026-11-01", AmountSen: 4590, CategoryName: "Cloud & hosting", Payee: "Cloudflare", Method: ledger.MethodCard,
			Note: sp("R2, \"domain\"\nline2"), Status: ledger.StatusActive, ReceiptSHA256: &shaPDF},
		{ID: 2, Direction: ledger.DirectionIncome, OccurredOn: "2026-11-02", AmountSen: 123456, CategoryName: "Client revenue", Payee: "=cmd|' /C calc'!A0", Method: ledger.MethodTNGEWallet,
			Status: ledger.StatusAmended},
		{ID: 3, Direction: ledger.DirectionIncome, OccurredOn: "2026-11-03", AmountSen: 1, CategoryName: "Other", Payee: "Kedai 茶", Method: ledger.MethodBankTransfer,
			Status: ledger.StatusVoided, VoidReason: sp("dup"), ReceiptSHA256: &shaMissing},
		{ID: 4, OccurredOn: "2026-11-04", AmountSen: 100, CategoryName: "Cloud & hosting", Payee: "Again", Method: ledger.MethodGrabPay,
			Status: ledger.StatusActive, ReceiptSHA256: &shaPDF},
	}
}

func TestWriteCSV(t *testing.T) {
	shaPDF, shaMissing := strings.Repeat("a", 64), strings.Repeat("b", 64)
	rs := fakeReceipts{shaPDF: "/x/" + shaPDF + ".pdf"}
	var buf bytes.Buffer
	if err := WriteCSV(&buf, sampleRows(shaPDF, shaMissing), rs); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "date,category,payee,method,amount_myr,note,status,entry_id,receipt_file,type\r\n") {
		t.Fatalf("header: %q", buf.String()[:80])
	}
	recs, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		Header,
		{"2026-11-01", "Cloud & hosting", "Cloudflare", "Card", "45.90", "R2, \"domain\"\nline2", "active", "1", shaPDF + ".pdf", "expense"},
		// income: amount stays positive, type column says income
		{"2026-11-02", "Client revenue", "'=cmd|' /C calc'!A0", "TNG eWallet", "1234.56", "", "amended", "2", "", "income"},
		{"2026-11-03", "Other", "Kedai 茶", "Bank transfer", "0.01", "", "voided", "3", shaMissing, "income"},
		// Direction "" (zero value) exports as expense
		{"2026-11-04", "Cloud & hosting", "Again", "GrabPay", "1.00", "", "active", "4", shaPDF + ".pdf", "expense"},
	}
	if len(recs) != len(want) {
		t.Fatalf("got %d records", len(recs))
	}
	for i := range want {
		if strings.Join(recs[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d = %q\nwant      %q", i, recs[i], want[i])
		}
	}
	// Header-only for empty export.
	buf.Reset()
	if err := WriteCSV(&buf, nil, nil); err != nil || buf.String() != strings.Join(Header, ",")+"\r\n" {
		t.Fatalf("empty: %q %v", buf.String(), err)
	}
}

func TestWriteZip(t *testing.T) {
	dir := t.TempDir()
	rstore, err := receipts.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.4\n%%EOF\n")
	info, err := rstore.Save(bytes.NewReader(pdf), -1)
	if err != nil {
		t.Fatal(err)
	}
	shaMissing := strings.Repeat("b", 64)
	cases := []struct {
		name      string
		rows      []ledger.Entry
		wantFiles []string
	}{
		{"receipts deduped + missing listed", sampleRows(info.SHA256, shaMissing),
			[]string{"expenses.csv", "receipts/" + info.FileName(), "MISSING_RECEIPTS.txt"}},
		{"no receipts", sampleRows(info.SHA256, shaMissing)[1:2], []string{"expenses.csv"}},
		{"empty", nil, []string{"expenses.csv"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteZip(&buf, tc.rows, rstore); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			files := map[string][]byte{}
			for _, f := range zr.File {
				names = append(names, f.Name)
				rc, _ := f.Open()
				files[f.Name], _ = io.ReadAll(rc)
				rc.Close()
			}
			if strings.Join(names, ",") != strings.Join(tc.wantFiles, ",") {
				t.Fatalf("files = %v, want %v", names, tc.wantFiles)
			}
			if r, ok := files["receipts/"+info.FileName()]; ok && !bytes.Equal(r, pdf) {
				t.Fatal("receipt content differs")
			}
			if m, ok := files["MISSING_RECEIPTS.txt"]; ok && !strings.Contains(string(m), shaMissing) {
				t.Fatal("missing list wrong")
			}
			var csvBuf bytes.Buffer
			WriteCSV(&csvBuf, tc.rows, rstore)
			if !bytes.Equal(files["expenses.csv"], csvBuf.Bytes()) {
				t.Fatal("zip csv differs from WriteCSV")
			}
		})
	}
}

var _ Receipts = (*receipts.Store)(nil)
