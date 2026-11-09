package ledger

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidateAmount(t *testing.T) {
	cases := []struct {
		sen int64
		ok  bool
	}{
		{math.MinInt64, false}, {-1, false}, {0, false}, {1, true}, {4590, true},
		{999_999_999, true}, {1_000_000_000, false}, {math.MaxInt64, false},
	}
	for _, tc := range cases {
		if err := ValidateAmount(tc.sen); (err == nil) != tc.ok {
			t.Errorf("ValidateAmount(%d) = %v, want ok=%v", tc.sen, err, tc.ok)
		}
	}
}

func TestValidateInput(t *testing.T) {
	today := time.Date(2026, 11, 9, 23, 30, 0, 0, time.FixedZone("MYT", 8*3600))
	good := Input{OccurredOn: "2026-11-09", AmountSen: 4590, CategoryID: 1, Payee: "Cloudflare", Method: MethodCard}
	cases := []struct {
		name      string
		mod       func(*Input)
		wantField string // "" = no field error
		wantErr   error
		check     func(t *testing.T, out Input)
	}{
		{"good", func(*Input) {}, "", nil, func(t *testing.T, out Input) {
			if out.Direction != DirectionExpense {
				t.Fatalf("direction default = %q", out.Direction)
			}
		}},
		{"amount zero", func(i *Input) { i.AmountSen = 0 }, "amount_sen", nil, nil},
		{"amount negative", func(i *Input) { i.AmountSen = -5 }, "amount_sen", nil, nil},
		{"amount max", func(i *Input) { i.AmountSen = MaxAmountSen }, "amount_sen", nil, nil},
		{"amount max-1", func(i *Input) { i.AmountSen = MaxAmountSen - 1 }, "", nil, nil},
		{"amount one sen", func(i *Input) { i.AmountSen = 1 }, "", nil, nil},
		{"payee empty", func(i *Input) { i.Payee = "" }, "payee", nil, nil},
		{"payee spaces", func(i *Input) { i.Payee = "   " }, "payee", nil, nil},
		{"payee trimmed", func(i *Input) { i.Payee = "  Grab  " }, "", nil, func(t *testing.T, out Input) {
			if out.Payee != "Grab" {
				t.Fatalf("payee = %q", out.Payee)
			}
		}},
		{"payee 120 multibyte", func(i *Input) { i.Payee = strings.Repeat("茶", 120) }, "", nil, nil},
		{"payee 121", func(i *Input) { i.Payee = strings.Repeat("a", 121) }, "payee", nil, nil},
		{"payee invalid utf8", func(i *Input) { i.Payee = "a\xffb" }, "payee", nil, nil},
		{"payee NUL", func(i *Input) { i.Payee = "a\x00b" }, "payee", nil, nil},
		{"note 500", func(i *Input) { i.Note = sp(strings.Repeat("n", 500)) }, "", nil, nil},
		{"note 501", func(i *Input) { i.Note = sp(strings.Repeat("n", 501)) }, "note", nil, nil},
		{"note empty to nil", func(i *Input) { i.Note = sp("  ") }, "", nil, func(t *testing.T, out Input) {
			if out.Note != nil {
				t.Fatal("empty note not nil")
			}
		}},
		{"method bad", func(i *Input) { i.Method = "paypal" }, "method", nil, nil},
		{"method empty", func(i *Input) { i.Method = "" }, "method", nil, nil},
		{"method tng", func(i *Input) { i.Method = MethodTNGEWallet }, "", nil, nil},
		{"category zero", func(i *Input) { i.CategoryID = 0 }, "category_id", nil, nil},
		{"direction bad", func(i *Input) { i.Direction = "refund" }, "direction", nil, nil},
		{"date bad format", func(i *Input) { i.OccurredOn = "2026-1-9" }, "occurred_on", nil, nil},
		{"date impossible", func(i *Input) { i.OccurredOn = "2026-02-30" }, "occurred_on", nil, nil},
		{"date empty", func(i *Input) { i.OccurredOn = "" }, "occurred_on", nil, nil},
		{"date rfc3339", func(i *Input) { i.OccurredOn = "2026-11-09T00:00:00Z" }, "occurred_on", nil, nil},
		{"date +365", func(i *Input) { i.OccurredOn = "2027-11-09" }, "", nil, nil},
		{"date -365", func(i *Input) { i.OccurredOn = "2025-11-09" }, "", nil, nil},
		{"date +366", func(i *Input) { i.OccurredOn = "2027-11-10" }, "", ErrDateConfirmRequired, nil},
		{"date -366", func(i *Input) { i.OccurredOn = "2025-11-08" }, "", ErrDateConfirmRequired, nil},
		{"date old confirmed", func(i *Input) { i.OccurredOn = "2019-01-01"; i.ConfirmDate = true }, "", nil, nil},
		{"receipt bad", func(i *Input) { i.Receipt("ABC") }, "receipt_sha256", nil, nil},
		{"receipt upper hex", func(i *Input) { i.Receipt(strings.Repeat("A", 64)) }, "receipt_sha256", nil, nil},
		{"receipt good", func(i *Input) { i.Receipt(strings.Repeat("a", 64)) }, "", nil, nil},
		{"receipt empty rejected", func(i *Input) { i.Receipt("") }, "receipt_sha256", nil, nil},
		{"receipt nil ok", func(i *Input) { i.ReceiptSHA256 = nil }, "", nil, nil},
		{"direction income rejected", func(i *Input) { i.Direction = DirectionIncome }, "direction", nil, nil},
		{"direction expense", func(i *Input) { i.Direction = DirectionExpense }, "", nil, nil},
		{"note trimmed", func(i *Input) { i.Note = sp("  hi \n") }, "", nil, func(t *testing.T, out Input) {
			if out.Note == nil || *out.Note != "hi" {
				t.Fatalf("note = %v", out.Note)
			}
		}},
		{"field errors beat date confirm", func(i *Input) { i.OccurredOn = "2030-01-01"; i.AmountSen = 0 }, "amount_sen", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := good
			tc.mod(&in)
			out, err := ValidateInput(in, today)
			var ve *ValidationError
			switch {
			case tc.wantField != "":
				if !errors.As(err, &ve) || ve.Fields[tc.wantField] == "" {
					t.Fatalf("err = %v, want field %s", err, tc.wantField)
				}
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected err %v", err)
				}
				if tc.check != nil {
					tc.check(t, out)
				}
			}
		})
	}
}

// Receipt is a tiny test helper.
func (i *Input) Receipt(s string) { i.ReceiptSHA256 = &s }

func TestValidateReason(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"Duplicate of #123", "Duplicate of #123", true},
		{"  dup  ", "dup", true},
		{"", "", false},
		{"   ", "", false},
		{strings.Repeat("r", 300), strings.Repeat("r", 300), true},
		{strings.Repeat("r", 301), "", false},
	}
	for _, tc := range cases {
		got, err := ValidateReason(tc.in)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("ValidateReason(%q) = %q, %v", tc.in, got, err)
		}
	}
}

func TestValidateFilterAndMonth(t *testing.T) {
	cases := []struct {
		name string
		f    Filter
		ok   bool
	}{
		{"empty", Filter{}, true},
		{"dates", Filter{From: "2026-01-01", To: "2026-12-31"}, true},
		{"bad from", Filter{From: "2026/01/01"}, false},
		{"bad to", Filter{To: "x"}, false},
		{"method", Filter{Method: MethodGrabPay}, true},
		{"bad method", Filter{Method: "x"}, false},
		{"status voided", Filter{Status: StatusVoided}, true},
		{"status amended not allowed", Filter{Status: StatusAmended}, false},
		{"neg category", Filter{CategoryID: -1}, false},
	}
	for _, tc := range cases {
		_, err := ValidateFilter(tc.f)
		if (err == nil) != tc.ok || (err != nil && !errors.Is(err, ErrBadRequest)) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
	for _, m := range []struct {
		s  string
		ok bool
	}{{"2026-11", true}, {"2026-13", false}, {"2026-1", false}, {"", false}, {"2026-11-01", false}} {
		if _, err := ParseMonth(m.s); (err == nil) != m.ok {
			t.Errorf("ParseMonth(%q) = %v", m.s, err)
		}
	}
}
