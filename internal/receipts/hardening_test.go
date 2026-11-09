package receipts

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pngHeader returns a PNG with only signature + IHDR(w,h) + IEND: the header
// claims a huge image but there is no pixel data.
func pngHeader(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 2 // 8-bit RGB
	chunk := func(typ string, data []byte) []byte {
		c := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
		c = append(c, typ...)
		c = append(c, data...)
		return binary.BigEndian.AppendUint32(c, crc32.ChecksumIEEE(append([]byte(typ), data...)))
	}
	out := []byte("\x89PNG\r\n\x1a\n")
	out = append(out, chunk("IHDR", ihdr)...)
	return append(out, chunk("IEND", nil)...)
}

// jpegHeader returns a real 8x8 JPEG whose SOF0 header is patched to claim w x h.
func jpegHeader(t *testing.T, w, h uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	i := bytes.Index(b, []byte{0xFF, 0xC0})
	if i < 0 {
		t.Fatal("no SOF0")
	}
	binary.BigEndian.PutUint16(b[i+5:], h)
	binary.BigEndian.PutUint16(b[i+7:], w)
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width != int(w) || cfg.Height != int(h) {
		t.Fatalf("patched header: %+v %v", cfg, err)
	}
	return b
}

func TestHugeDimensionsRejectedBeforeDecode(t *testing.T) {
	var decodes atomic.Int32
	old := beforeDecode
	beforeDecode = func() { decodes.Add(1) }
	t.Cleanup(func() { beforeDecode = old })

	cases := []struct {
		name string
		raw  []byte
	}{
		{"png 100000x100000", pngHeader(100000, 100000)},
		{"png 5001x5000 (just over 25MP)", pngHeader(5001, 5000)},
		{"jpeg 65535x65535", jpegHeader(t, 65535, 65535)},
		{"jpeg 6000x5000", jpegHeader(t, 6000, 5000)},
	}
	for _, tc := range cases {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(tc.raw))
		if err != nil || int64(cfg.Width)*int64(cfg.Height) <= MaxPixels {
			t.Fatalf("%s: header not oversized: %+v %v", tc.name, cfg, err)
		}
	}
	s := newStore(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Save(bytes.NewReader(tc.raw), -1); !errors.Is(err, ErrInvalidImage) {
				t.Fatalf("err = %v, want ErrInvalidImage", err)
			}
		})
	}
	if n := decodes.Load(); n != 0 {
		t.Fatalf("%d full decodes attempted for oversized headers", n)
	}
	if MaxPixels != 25_000_000 {
		t.Fatalf("MaxPixels = %d", MaxPixels)
	}
	// A normal image does reach the decoder.
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	if _, err := s.Save(&buf, -1); err != nil || decodes.Load() != 1 {
		t.Fatalf("small png: %v decodes=%d", err, decodes.Load())
	}
}

func TestDecodeIsSerialised(t *testing.T) {
	var cur, peak atomic.Int32
	old := beforeDecode
	beforeDecode = func() {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		cur.Add(-1)
	}
	t.Cleanup(func() { beforeDecode = old })
	s := newStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Save(bytes.NewReader(jpegWithExif(t, 1)), -1); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() != 1 {
		t.Fatalf("peak concurrent decodes = %d, want 1", peak.Load())
	}
}

func TestOpenVerified(t *testing.T) {
	cases := []struct {
		name    string
		tamper  func(t *testing.T, path string)
		sha     func(real string) string
		wantErr error
	}{
		{"intact", nil, nil, nil},
		{"overwritten", func(t *testing.T, p string) { os.WriteFile(p, []byte("%PDF-1.4 forged"), 0o600) }, nil, ErrTampered},
		{"truncated", func(t *testing.T, p string) { os.Truncate(p, 3) }, nil, ErrTampered},
		{"deleted", func(t *testing.T, p string) { os.Remove(p) }, nil, ErrNotFound},
		{"bad sha", nil, func(string) string { return "zz" }, ErrInvalidSHA},
		{"unknown", nil, func(string) string { return strings.Repeat("1", 64) }, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			info, err := s.Save(bytes.NewReader(pdfBytes), -1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.tamper != nil {
				tc.tamper(t, filepath.Join(s.Dir(), info.FileName()))
			}
			sha := info.SHA256
			if tc.sha != nil {
				sha = tc.sha(sha)
			}
			f, got, err := s.OpenVerified(sha)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if verr := s.VerifyFile(sha); !errors.Is(verr, tc.wantErr) {
				t.Fatalf("VerifyFile err = %v, want %v", verr, tc.wantErr)
			}
			if err != nil {
				if f != nil {
					t.Fatal("file returned with error")
				}
				return
			}
			defer f.Close()
			b, _ := io.ReadAll(f)
			if !bytes.Equal(b, pdfBytes) || got != info {
				t.Fatalf("content/info mismatch: %+v", got)
			}
		})
	}
	if !errors.Is(ErrCorrupt, ErrTampered) {
		t.Fatal("ErrCorrupt alias")
	}
}
