// Package export writes the FR-12 / TR-10 CSV and zip exports from
// current-view entries (ledger.Service.ExportEntries).
//
// CSV: header exactly Header; RFC 4180 via encoding/csv with CRLF line
// endings; amount_myr formatted from integer sen with integer math only
// ("1234.56", no separators, no currency); method as its display label;
// status active|amended|voided; receipt_file "<sha256>.<ext>" or empty.
// Any cell starting with '=', '+', '-', '@', TAB or CR is prefixed with '
// (CSV/formula injection guard).
//
// Zip: expenses.csv plus receipts/<sha256>.<ext> for every distinct
// receipt referenced by the rows (voided rows included). Referenced receipts
// whose file is missing are listed in MISSING_RECEIPTS.txt instead.
package export

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/JaxzTan/kira/internal/ledger"
	"github.com/JaxzTan/kira/internal/receipts"
)

// Header is the exact CSV header (TR-10).
var Header = []string{"date", "category", "payee", "method", "amount_myr", "note", "status", "entry_id", "receipt_file"}

// Receipts resolves a receipt sha to its file (*receipts.Store satisfies it).
type Receipts interface {
	Path(sha string) (string, receipts.Info, error)
}

// FormatSen renders sen as "1234.56" using integer math only.
func FormatSen(sen int64) string {
	neg := sen < 0
	u := uint64(sen)
	if neg {
		u = uint64(-(sen + 1)) + 1 // safe for MinInt64
	}
	s := strconv.FormatUint(u/100, 10) + "." + fmt.Sprintf("%02d", u%100)
	if neg {
		return "-" + s
	}
	return s
}

// guard neutralises spreadsheet formula injection.
func guard(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func receiptFile(e ledger.Entry, rs Receipts) string {
	if e.ReceiptSHA256 == nil {
		return ""
	}
	sha := *e.ReceiptSHA256
	if rs != nil {
		if _, info, err := rs.Path(sha); err == nil {
			return info.FileName()
		}
	}
	return sha // file missing: keep the reference
}

// Record returns the CSV cells for one entry (already injection-guarded).
func Record(e ledger.Entry, rs Receipts) []string {
	label := ledger.MethodLabel(e.Method)
	if label == "" {
		label = e.Method
	}
	note := ""
	if e.Note != nil {
		note = *e.Note
	}
	cells := []string{
		e.OccurredOn, e.CategoryName, e.Payee, label, FormatSen(e.AmountSen), note, e.Status,
		strconv.FormatInt(e.ID, 10), receiptFile(e, rs),
	}
	for i := range cells {
		cells[i] = guard(cells[i])
	}
	return cells
}

// WriteCSV writes header + one row per entry, in the given order.
func WriteCSV(w io.Writer, rows []ledger.Entry, rs Receipts) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	if err := cw.Write(Header); err != nil {
		return err
	}
	for _, e := range rows {
		if err := cw.Write(Record(e, rs)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteZip writes expenses.csv and the referenced receipt files.
func WriteZip(w io.Writer, rows []ledger.Entry, rs Receipts) error {
	zw := zip.NewWriter(w)
	now := time.Now()
	create := func(name string) (io.Writer, error) {
		return zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: now})
	}
	f, err := create("expenses.csv")
	if err != nil {
		return err
	}
	if err := WriteCSV(f, rows, rs); err != nil {
		return err
	}
	seen := map[string]bool{}
	var missing []string
	for _, e := range rows {
		if e.ReceiptSHA256 == nil || seen[*e.ReceiptSHA256] {
			continue
		}
		sha := *e.ReceiptSHA256
		seen[sha] = true
		if rs == nil {
			missing = append(missing, sha)
			continue
		}
		path, info, err := rs.Path(sha)
		if err != nil {
			missing = append(missing, sha)
			continue
		}
		if err := addFile(create, "receipts/"+info.FileName(), path); err != nil {
			if os.IsNotExist(err) {
				missing = append(missing, sha)
				continue
			}
			return err
		}
	}
	if len(missing) > 0 {
		f, err := create("MISSING_RECEIPTS.txt")
		if err != nil {
			return err
		}
		if _, err := io.WriteString(f, "Receipts referenced by the ledger but not found on disk:\n"+strings.Join(missing, "\n")+"\n"); err != nil {
			return err
		}
	}
	return zw.Close()
}

func addFile(create func(string) (io.Writer, error), name, path string) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := create(name)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return err
}
