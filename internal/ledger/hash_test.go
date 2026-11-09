package ledger

import (
	"strings"
	"testing"
)

func exampleRow() Row {
	amt, cat := int64(4590), int64(1)
	return Row{
		ID: 1, Kind: KindCreate, Direction: DirectionExpense, OccurredOn: sp("2026-11-09"),
		AmountSen: &amt, Currency: CurrencyMYR, CategoryID: &cat, Payee: sp("Cloudflare"),
		Method: sp(MethodCard), Note: sp("R2 + domain"), CreatedAt: "2026-11-09T03:12:44Z",
		PrevHash: GenesisHash, RefID: nil,
	}
}

func TestCanonicalJSON(t *testing.T) {
	base := exampleRow()
	cases := []struct {
		name string
		mod  func(*Row)
		want string // substring expected in canonical form
	}{
		{"golden", func(*Row) {}, `{"amount_sen":4590,"category_id":1,"created_at":"2026-11-09T03:12:44Z","currency":"MYR","direction":"expense","id":1,"kind":"create","method":"card","note":"R2 + domain","occurred_on":"2026-11-09","payee":"Cloudflare","prev_hash":"` + GenesisHash + `","receipt_sha256":null,"ref_id":null,"void_reason":null}`},
		{"html not escaped", func(r *Row) { r.Payee = sp("A&B <x>") }, `"payee":"A&B <x>"`},
		{"quotes and backslash", func(r *Row) { r.Note = sp(`say "hi" \ ok`) }, `"note":"say \"hi\" \\ ok"`},
		{"control chars", func(r *Row) { r.Note = sp("a\nb\tc\x01") }, `"note":"a\nb\tc\u0001"`},
		{"unicode literal", func(r *Row) { r.Payee = sp("Kedai Mamak — 茶") }, `"payee":"Kedai Mamak — 茶"`},
		{"line separator escaped", func(r *Row) { r.Note = sp("a\u2028b") }, `"note":"a\u2028b"`},
		{"void row nulls", func(r *Row) {
			ref := int64(1)
			r.Kind, r.RefID, r.AmountSen, r.CategoryID, r.OccurredOn, r.Payee, r.Method, r.Note = KindVoid, &ref, nil, nil, nil, nil, nil, nil
			r.VoidReason = sp("dup")
		}, `{"amount_sen":null,"category_id":null,`},
		{"ref id", func(r *Row) { ref := int64(126); r.RefID = &ref }, `"ref_id":126,`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.mod(&r)
			got, err := canonicalJSON(rowFields(r))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "golden" && string(got) != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
			if !strings.Contains(string(got), tc.want) {
				t.Fatalf("got %s, want substring %s", got, tc.want)
			}
		})
	}
}

func TestCanonicalJSONRejects(t *testing.T) {
	f := rowFields(exampleRow())
	f["amount_sen"] = 45.9
	if _, err := canonicalJSON(f); err == nil {
		t.Fatal("float accepted")
	}
	f = rowFields(exampleRow())
	delete(f, "note")
	if _, err := canonicalJSON(f); err == nil {
		t.Fatal("missing key accepted")
	}
}

// Golden vector computed independently (Python hmac/hashlib) over the
// canonical example with key = 32 bytes of 0x42. Locks the chain format.
func TestRowHashGolden(t *testing.T) {
	got, err := RowHash(testKey, exampleRow())
	if err != nil {
		t.Fatal(err)
	}
	if got != goldenHash {
		t.Fatalf("hash = %s, want %s", got, goldenHash)
	}
	other, _ := RowHash([]byte(strings.Repeat("k", 32)), exampleRow())
	if other == got {
		t.Fatal("hash independent of key")
	}
}

const goldenHash = "bf93b300802dbff046d9960c4ea53c9640c00ad6214b2ce637d9611705894b3c"
