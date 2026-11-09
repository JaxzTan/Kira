package ledger

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// Head file (DATA_DIR/head) authentication. kira-api owns the file; the
// crypto lives here with the chain.
//
//	headKey = HKDF-SHA256(secret = LEDGER_KEY, salt = none, info = "kira-head", length = 32)
//	mac     = lowercase_hex( HMAC-SHA256(headKey, "head|<id>|<hash>") )
//	file    = "<id>:<hash>:<mac>\n"
//
// HKDF is the Go standard library's crypto/hkdf (RFC 5869; an absent salt
// means a zero-filled salt of hash length), so no new module is needed.

// HeadMACInfo is the HKDF info string for the head-file key.
const HeadMACInfo = "kira-head"

// ErrHeadInvalid: the head file line is malformed or its MAC does not verify.
var ErrHeadInvalid = errors.New("head file invalid")

func headKey(ledgerKey []byte) []byte {
	k, err := hkdf.Key(sha256.New, ledgerKey, nil, HeadMACInfo, 32)
	if err != nil { // only possible for absurd lengths
		panic("ledger: hkdf: " + err.Error())
	}
	return k
}

// HeadMAC returns hex(HMAC-SHA256(HKDF(ledgerKey, "kira-head"), "head|<id>|<hash>")).
func HeadMAC(ledgerKey []byte, id int64, hash string) string {
	m := hmac.New(sha256.New, headKey(ledgerKey))
	m.Write([]byte("head|" + strconv.FormatInt(id, 10) + "|" + hash))
	return hex.EncodeToString(m.Sum(nil))
}

// CheckHeadMAC reports, in constant time, whether mac is HeadMAC(ledgerKey, id, hash).
func CheckHeadMAC(ledgerKey []byte, id int64, hash, mac string) bool {
	want := HeadMAC(ledgerKey, id, hash)
	return hmac.Equal([]byte(want), []byte(mac))
}

// FormatHeadFile renders the head file content "id:hash:mac\n".
func FormatHeadFile(ledgerKey []byte, h Head) string {
	return FormatHeadID(h) + ":" + HeadMAC(ledgerKey, h.ID, h.Hash) + "\n"
}

// ParseHeadFile parses and authenticates head file content (surrounding
// whitespace ignored). Any malformed line or bad MAC returns ErrHeadInvalid;
// callers then verify with VerifyOptions{HeadReason: ReasonHeadInvalid}
// (or ReasonHeadMissing when the file does not exist).
func ParseHeadFile(ledgerKey []byte, content string) (Head, error) {
	parts := strings.Split(strings.TrimSpace(content), ":")
	if len(parts) != 3 {
		return Head{}, ErrHeadInvalid
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id < 0 || strconv.FormatInt(id, 10) != parts[0] || !IsSHA256Hex(parts[1]) || !IsSHA256Hex(parts[2]) {
		return Head{}, ErrHeadInvalid
	}
	if !CheckHeadMAC(ledgerKey, id, parts[1], parts[2]) {
		return Head{}, ErrHeadInvalid
	}
	return Head{ID: id, Hash: parts[1]}, nil
}
