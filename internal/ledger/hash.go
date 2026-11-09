package ledger

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// hashedColumns is every entries column except hash, in canonical (sorted) order.
var hashedColumns = []string{
	"amount_sen", "category_id", "created_at", "currency", "direction", "id", "kind",
	"method", "note", "occurred_on", "payee", "prev_hash", "receipt_sha256", "ref_id", "void_reason",
}

func init() {
	if !sort.StringsAreSorted(hashedColumns) {
		panic("ledger: hashedColumns not sorted")
	}
}

var errColumnType = errors.New("invalid column type")

// canonicalJSON encodes fields (values nil, int64 or string) per the package doc.
// Every key in hashedColumns must be present.
func canonicalJSON(fields map[string]any) ([]byte, error) {
	if len(fields) != len(hashedColumns) {
		return nil, fmt.Errorf("ledger: canonical: got %d fields, want %d", len(fields), len(hashedColumns))
	}
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range hashedColumns {
		v, ok := fields[k]
		if !ok {
			return nil, fmt.Errorf("ledger: canonical: missing %s", k)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(k) // keys are plain ASCII identifiers
		b.WriteString(`":`)
		switch x := v.(type) {
		case nil:
			b.WriteString("null")
		case int64:
			b.WriteString(strconv.FormatInt(x, 10))
		case string:
			if err := writeJSONString(&b, x); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%w: %s is %T", errColumnType, k, v)
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func writeJSONString(b *bytes.Buffer, s string) error {
	var tmp bytes.Buffer
	enc := json.NewEncoder(&tmp)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	b.Write(bytes.TrimSuffix(tmp.Bytes(), []byte{'\n'}))
	return nil
}

// computeHash returns hex(HMAC_SHA256(key, prevHash || canonical)).
func computeHash(key []byte, prevHash string, canonical []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(prevHash))
	m.Write(canonical)
	return hex.EncodeToString(m.Sum(nil))
}

func ptrVal[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// rowFields maps a Row to its hashed columns.
func rowFields(r Row) map[string]any {
	return map[string]any{
		"amount_sen":     ptrVal(r.AmountSen),
		"category_id":    ptrVal(r.CategoryID),
		"created_at":     r.CreatedAt,
		"currency":       r.Currency,
		"direction":      r.Direction,
		"id":             r.ID,
		"kind":           r.Kind,
		"method":         ptrVal(r.Method),
		"note":           ptrVal(r.Note),
		"occurred_on":    ptrVal(r.OccurredOn),
		"payee":          ptrVal(r.Payee),
		"prev_hash":      r.PrevHash,
		"receipt_sha256": ptrVal(r.ReceiptSHA256),
		"ref_id":         ptrVal(r.RefID),
		"void_reason":    ptrVal(r.VoidReason),
	}
}

// RowHash computes the chain hash of r (r.Hash is ignored).
func RowHash(key []byte, r Row) (string, error) {
	c, err := canonicalJSON(rowFields(r))
	if err != nil {
		return "", err
	}
	return computeHash(key, r.PrevHash, c), nil
}
