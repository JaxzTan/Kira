package receipts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gpsMarker = "SECRETGPS 3.139N 101.687E"

// halfImage is 64x32: left half red, right half blue.
func halfImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 64; x++ {
			c := color.RGBA{220, 20, 20, 255}
			if x >= 32 {
				c = color.RGBA{20, 20, 220, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// exifTIFF builds a little-endian TIFF with Orientation and a GPS IFD.
func exifTIFF(orientation uint16) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	w := func(v any) { binary.Write(&b, le, v) }
	b.WriteString("II")
	w(uint16(42))
	w(uint32(8))
	w(uint16(2)) // IFD0 entries
	w(uint16(0x0112))
	w(uint16(3))
	w(uint32(1))
	w(orientation)
	w(uint16(0))
	w(uint16(0x8825)) // GPSInfo pointer
	w(uint16(4))
	w(uint32(1))
	w(uint32(38))
	w(uint32(0)) // next IFD
	w(uint16(1)) // GPS IFD @38
	w(uint16(0x0002))
	w(uint16(2))
	w(uint32(len(gpsMarker) + 1))
	w(uint32(56))
	w(uint32(0))
	b.WriteString(gpsMarker + "\x00") // @56
	return b.Bytes()
}

func jpegWithExif(t *testing.T, orientation uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, halfImage(), &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	payload := append([]byte("Exif\x00\x00"), exifTIFF(orientation)...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	seg = append(seg, payload...)
	out := append([]byte{}, raw[:2]...)
	out = append(out, seg...)
	return append(out, raw[2:]...)
}

func pngWithMeta(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, halfImage()); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	chunk := func(typ string, data []byte) []byte {
		c := make([]byte, 4)
		binary.BigEndian.PutUint32(c, uint32(len(data)))
		c = append(c, typ...)
		c = append(c, data...)
		crc := make([]byte, 4)
		binary.BigEndian.PutUint32(crc, crc32.ChecksumIEEE(append([]byte(typ), data...)))
		return append(c, crc...)
	}
	iend := raw[len(raw)-12:]
	out := append([]byte{}, raw[:len(raw)-12]...)
	out = append(out, chunk("tEXt", []byte("Comment\x00"+gpsMarker))...)
	out = append(out, chunk("eXIf", exifTIFF(1))...)
	return append(out, iend...)
}

var pdfBytes = []byte("%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "data", "receipts"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSave(t *testing.T) {
	jpg := jpegWithExif(t, 1)
	jpgRot := jpegWithExif(t, 6)
	pngMeta := pngWithMeta(t)
	cases := []struct {
		name     string
		data     []byte
		declared int64
		wantErr  error
		wantExt  string
		wantType string
		check    func(t *testing.T, stored []byte)
	}{
		{"jpeg strips exif+gps", jpg, int64(len(jpg)), nil, "jpg", "image/jpeg", func(t *testing.T, b []byte) {
			if bytes.Contains(b, []byte("Exif\x00\x00")) || bytes.Contains(b, []byte(gpsMarker)) {
				t.Fatal("EXIF/GPS survived re-encode")
			}
			img, err := jpeg.Decode(bytes.NewReader(b))
			if err != nil || img.Bounds().Dx() != 64 || img.Bounds().Dy() != 32 {
				t.Fatalf("decode: %v %v", err, img.Bounds())
			}
		}},
		{"jpeg orientation 6 applied", jpgRot, -1, nil, "jpg", "image/jpeg", func(t *testing.T, b []byte) {
			img, err := jpeg.Decode(bytes.NewReader(b))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != 32 || img.Bounds().Dy() != 64 {
				t.Fatalf("bounds %v, want 32x64", img.Bounds())
			}
			r, _, bl, _ := img.At(16, 8).RGBA()
			r2, _, b2, _ := img.At(16, 56).RGBA()
			if r <= bl || b2 <= r2 {
				t.Fatalf("rotation wrong: top r=%d b=%d bottom r=%d b=%d", r, bl, r2, b2)
			}
		}},
		{"png strips text+exif", pngMeta, -1, nil, "png", "image/png", func(t *testing.T, b []byte) {
			if bytes.Contains(b, []byte("eXIf")) || bytes.Contains(b, []byte("tEXt")) || bytes.Contains(b, []byte(gpsMarker)) {
				t.Fatal("PNG metadata survived")
			}
		}},
		{"pdf stored as-is", pdfBytes, int64(len(pdfBytes)), nil, "pdf", "application/pdf", func(t *testing.T, b []byte) {
			if !bytes.Equal(b, pdfBytes) {
				t.Fatal("pdf changed")
			}
		}},
		{"gif rejected", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;"), -1, ErrUnsupported, "", "", nil},
		{"html rejected", []byte("<html><script>alert(1)</script></html>"), -1, ErrUnsupported, "", "", nil},
		{"text rejected", []byte("hello"), -1, ErrUnsupported, "", "", nil},
		{"empty rejected", nil, 0, ErrUnsupported, "", "", nil},
		{"truncated jpeg", jpg[:len(jpg)/3], -1, ErrInvalidImage, "", "", nil},
		{"png header only", []byte("\x89PNG\r\n\x1a\n\x00\x00"), -1, ErrInvalidImage, "", "", nil},
		{"declared too large", pdfBytes, MaxBytes + 1, ErrTooLarge, "", "", nil},
		{"actual too large", append(append([]byte{}, pdfBytes...), make([]byte, MaxBytes)...), -1, ErrTooLarge, "", "", nil},
		{"exactly max ok", append(append([]byte{}, pdfBytes...), make([]byte, MaxBytes-len(pdfBytes))...), MaxBytes, nil, "pdf", "application/pdf", nil},
	}
	s := newStore(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := s.Save(bytes.NewReader(tc.data), tc.declared)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if info.Ext != tc.wantExt || info.ContentType != tc.wantType || !ValidSHA(info.SHA256) {
				t.Fatalf("info = %+v", info)
			}
			p := filepath.Join(s.Dir(), info.SHA256+"."+info.Ext)
			stored, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(stored)) != info.Size {
				t.Fatalf("size %d != %d", len(stored), info.Size)
			}
			fi, _ := os.Stat(p)
			if fi.Mode().Perm() != 0o600 {
				t.Fatalf("file mode %o", fi.Mode().Perm())
			}
			if err := s.VerifyFile(info.SHA256); err != nil {
				t.Fatalf("verify: %v", err)
			}
			if tc.check != nil {
				tc.check(t, stored)
			}
		})
	}
	di, _ := os.Stat(s.Dir())
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", di.Mode().Perm())
	}
	ents, _ := os.ReadDir(s.Dir())
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".upload-") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
}

func TestSaveIdempotentAndRepair(t *testing.T) {
	s := newStore(t)
	a, err := s.Save(bytes.NewReader(pdfBytes), -1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Save(bytes.NewReader(pdfBytes), -1)
	if err != nil || a != b {
		t.Fatalf("second save: %+v %v", b, err)
	}
	jpg := jpegWithExif(t, 1)
	j1, _ := s.Save(bytes.NewReader(jpg), -1)
	j2, _ := s.Save(bytes.NewReader(jpg), -1)
	if j1.SHA256 != j2.SHA256 {
		t.Fatal("jpeg re-encode not deterministic")
	}
	// Swap file content on disk: detected, and a re-upload repairs it.
	p := filepath.Join(s.Dir(), a.FileName())
	if err := os.WriteFile(p, []byte("%PDF-1.4 forged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyFile(a.SHA256); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("swapped file: err = %v", err)
	}
	if _, err := s.Save(bytes.NewReader(pdfBytes), -1); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyFile(a.SHA256); err != nil {
		t.Fatalf("not repaired: %v", err)
	}
}

func TestOpenAndExists(t *testing.T) {
	s := newStore(t)
	info, err := s.Save(bytes.NewReader(pdfBytes), -1)
	if err != nil {
		t.Fatal(err)
	}
	// A file planted outside the dir must be unreachable.
	os.WriteFile(filepath.Join(filepath.Dir(s.Dir()), "secret.pdf"), []byte("x"), 0o600)
	cases := []struct {
		name    string
		sha     string
		wantErr error
	}{
		{"ok", info.SHA256, nil},
		{"unknown", strings.Repeat("0", 64), ErrNotFound},
		{"uppercase", strings.ToUpper(info.SHA256), ErrInvalidSHA},
		{"short", info.SHA256[:63], ErrInvalidSHA},
		{"traversal", "../secret", ErrInvalidSHA},
		{"traversal padded", "../../../../../../../../../../../../../../../../../../../etc/passwd", ErrInvalidSHA},
		{"with ext", info.SHA256 + ".pdf", ErrInvalidSHA},
		{"empty", "", ErrInvalidSHA},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, got, err := s.Open(tc.sha)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if s.Exists(tc.sha) != (tc.wantErr == nil) {
				t.Fatal("Exists mismatch")
			}
			if err != nil {
				return
			}
			defer f.Close()
			b, _ := io.ReadAll(f)
			if !bytes.Equal(b, pdfBytes) || got != info {
				t.Fatalf("open: %+v", got)
			}
			p, pi, err := s.Path(tc.sha)
			if err != nil || pi != info || filepath.Base(p) != info.FileName() {
				t.Fatalf("path: %s %+v %v", p, pi, err)
			}
		})
	}
}

func TestJPEGOrientationParser(t *testing.T) {
	for o := uint16(1); o <= 8; o++ {
		if got := jpegOrientation(jpegWithExif(t, o)); got != int(o) {
			t.Errorf("orientation %d parsed as %d", o, got)
		}
	}
	junk := [][]byte{nil, {0xFF}, {0xFF, 0xD8}, {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}, []byte("not a jpeg")}
	for _, b := range junk {
		if jpegOrientation(b) != 1 {
			t.Errorf("junk %q gave non-1", b)
		}
	}
	if jpegOrientation(jpegWithExif(t, 9)) != 1 {
		t.Error("out-of-range orientation accepted")
	}
}

func TestApplyOrientationDims(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.RGBA{255, 0, 0, 255}) // top-left marker
	cases := []struct {
		o          int
		w, h       int
		markX, mkY int
	}{
		{1, 3, 2, 0, 0}, {2, 3, 2, 2, 0}, {3, 3, 2, 2, 1}, {4, 3, 2, 0, 1},
		{5, 2, 3, 0, 0}, {6, 2, 3, 1, 0}, {7, 2, 3, 1, 2}, {8, 2, 3, 0, 2},
	}
	for _, tc := range cases {
		got := applyOrientation(src, tc.o)
		if got.Bounds().Dx() != tc.w || got.Bounds().Dy() != tc.h {
			t.Errorf("o=%d bounds %v", tc.o, got.Bounds())
			continue
		}
		if r, _, _, _ := got.At(tc.markX, tc.mkY).RGBA(); r != 0xFFFF {
			t.Errorf("o=%d marker not at (%d,%d)", tc.o, tc.markX, tc.mkY)
		}
	}
}
