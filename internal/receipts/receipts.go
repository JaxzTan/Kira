// Package receipts is Kira's content-addressed receipt store (FR-6, TR-8).
//
// Files live at <dir>/<sha256>.<ext> (ext jpg|png|pdf), mode 0600, dir 0700.
// Type is detected by content sniffing (http.DetectContentType), never by
// filename. JPEG and PNG uploads are decoded and re-encoded with the
// standard library encoders, which write no metadata, so EXIF (including
// GPS), XMP, ICC and PNG text/eXIf chunks are dropped; the JPEG EXIF
// orientation is applied to the pixels first so photos stay upright. PDFs
// are stored byte-for-byte. The sha256 is of the stored bytes, so the
// ledger's receipt_sha256 also proves the file was not swapped
// (see Store.VerifyFile).
package receipts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
)

// Limits.
const (
	MaxBytes  = 10 << 20   // upload limit (TR-8)
	MaxPixels = 60_000_000 // decompression-bomb guard
	// JPEGQuality is used when re-encoding JPEGs.
	JPEGQuality = 90
)

// Errors. Suggested HTTP mapping in comments.
var (
	ErrTooLarge     = errors.New("receipt exceeds 10 MB")               // 413
	ErrUnsupported  = errors.New("receipt must be JPEG, PNG or PDF")    // 415
	ErrInvalidImage = errors.New("receipt image could not be decoded")  // 415
	ErrInvalidSHA   = errors.New("receipt id must be 64 lowercase hex") // 400
	ErrNotFound     = errors.New("receipt not found")                   // 404
	ErrCorrupt      = errors.New("receipt file content does not match its hash")
)

// Info describes a stored receipt (api.Receipt).
type Info struct {
	SHA256      string
	Ext         string // jpg | png | pdf
	ContentType string // image/jpeg | image/png | application/pdf
	Size        int64  // stored bytes
}

// FileName is "<sha256>.<ext>".
func (i Info) FileName() string { return i.SHA256 + "." + i.Ext }

var kinds = []struct{ ext, ctype string }{
	{"jpg", "image/jpeg"},
	{"png", "image/png"},
	{"pdf", "application/pdf"},
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidSHA reports whether s is 64 lowercase hex characters.
func ValidSHA(s string) bool { return shaRe.MatchString(s) }

// Store is a receipts directory.
type Store struct{ dir string }

// New creates dir (mode 0700) if needed.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("receipts: empty dir")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("receipts: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("receipts: %w", err)
	}
	return &Store{dir: dir}, nil
}

// Dir returns the receipts directory.
func (s *Store) Dir() string { return s.dir }

// Save reads an upload (declaredSize < 0 if unknown), validates and
// normalises it, and stores it. Saving identical content is idempotent.
func (s *Store) Save(r io.Reader, declaredSize int64) (Info, error) {
	if declaredSize > MaxBytes {
		return Info{}, ErrTooLarge
	}
	raw, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Info{}, fmt.Errorf("receipts: read: %w", err)
	}
	if len(raw) > MaxBytes {
		return Info{}, ErrTooLarge
	}
	if len(raw) == 0 {
		return Info{}, ErrUnsupported
	}
	ctype := http.DetectContentType(raw)
	var data []byte
	var ext string
	switch ctype {
	case "image/jpeg":
		ext = "jpg"
		data, err = reencodeJPEG(raw)
	case "image/png":
		ext = "png"
		data, err = reencodePNG(raw)
	case "application/pdf":
		ext, data = "pdf", raw
	default:
		return Info{}, ErrUnsupported
	}
	if err != nil {
		return Info{}, err
	}
	sum := sha256.Sum256(data)
	info := Info{SHA256: hex.EncodeToString(sum[:]), Ext: ext, ContentType: ctype, Size: int64(len(data))}
	path := filepath.Join(s.dir, info.FileName())
	if err := s.checkFile(path, info.SHA256); err == nil {
		return info, nil // already stored
	}
	if err := writeAtomic(s.dir, path, data); err != nil {
		return Info{}, err
	}
	return info, nil
}

func checkPixels(raw []byte, decodeConfig func(io.Reader) (image.Config, error)) error {
	cfg, err := decodeConfig(bytes.NewReader(raw))
	if err != nil {
		return ErrInvalidImage
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return ErrInvalidImage
	}
	return nil
}

func reencodeJPEG(raw []byte) ([]byte, error) {
	if err := checkPixels(raw, jpeg.DecodeConfig); err != nil {
		return nil, err
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrInvalidImage
	}
	img = applyOrientation(img, jpegOrientation(raw))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, fmt.Errorf("receipts: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func reencodePNG(raw []byte) ([]byte, error) {
	if err := checkPixels(raw, png.DecodeConfig); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrInvalidImage
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("receipts: encode png: %w", err)
	}
	return buf.Bytes(), nil
}

func writeAtomic(dir, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return fmt.Errorf("receipts: temp: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op after successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("receipts: chmod: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("receipts: write: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("receipts: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("receipts: close: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("receipts: rename: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// Stat finds a stored receipt. Errors: ErrInvalidSHA, ErrNotFound.
func (s *Store) Stat(sha string) (Info, string, error) {
	if !ValidSHA(sha) {
		return Info{}, "", ErrInvalidSHA
	}
	for _, k := range kinds {
		p := filepath.Join(s.dir, sha+"."+k.ext)
		fi, err := os.Stat(p)
		if err == nil && fi.Mode().IsRegular() {
			return Info{SHA256: sha, Ext: k.ext, ContentType: k.ctype, Size: fi.Size()}, p, nil
		}
	}
	return Info{}, "", ErrNotFound
}

// Path returns the absolute file path and info for sha (used by export).
func (s *Store) Path(sha string) (string, Info, error) {
	info, p, err := s.Stat(sha)
	return p, info, err
}

// Exists reports whether a receipt with this sha is stored (ledger validation).
func (s *Store) Exists(sha string) bool {
	_, _, err := s.Stat(sha)
	return err == nil
}

// Open opens a stored receipt for serving. The caller closes the file.
func (s *Store) Open(sha string) (*os.File, Info, error) {
	info, p, err := s.Stat(sha)
	if err != nil {
		return nil, Info{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, Info{}, fmt.Errorf("receipts: open: %w", err)
	}
	return f, info, nil
}

// VerifyFile re-hashes a stored receipt; ErrCorrupt if the content was swapped.
func (s *Store) VerifyFile(sha string) error {
	_, p, err := s.Stat(sha)
	if err != nil {
		return err
	}
	return s.checkFile(p, sha)
}

func (s *Store) checkFile(path, sha string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != sha {
		return ErrCorrupt
	}
	return nil
}
