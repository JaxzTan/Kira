package ledger

import (
	"strconv"
	"time"
)

// Enum values (same strings as docs/api-contract.md).
const (
	KindCreate = "create"
	KindAmend  = "amend"
	KindVoid   = "void"

	DirectionExpense = "expense"
	DirectionIncome  = "income"

	StatusActive  = "active"
	StatusAmended = "amended"
	StatusVoided  = "voided"

	MethodCard         = "card"
	MethodCash         = "cash"
	MethodBankTransfer = "bank_transfer"
	MethodTNGEWallet   = "tng_ewallet"
	MethodGrabPay      = "grabpay"
	MethodOther        = "other"

	CurrencyMYR = "MYR"
)

// GenesisHash is prev_hash of entry 1 and head_hash of an empty ledger.
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Validation limits (TRD §7, api-contract).
const (
	MaxAmountSen     int64 = 1_000_000_000 // exclusive: < RM 10,000,000
	MaxPayeeLen            = 120
	MaxNoteLen             = 500
	MaxVoidReasonLen       = 300
	DateConfirmDays        = 365
)

// Methods lists payment methods in display order with labels.
var Methods = []struct{ Value, Label string }{
	{MethodCard, "Card"},
	{MethodCash, "Cash"},
	{MethodBankTransfer, "Bank transfer"},
	{MethodTNGEWallet, "TNG eWallet"},
	{MethodGrabPay, "GrabPay"},
	{MethodOther, "Other"},
}

// MethodLabel returns the display label for a method value ("" if unknown).
func MethodLabel(m string) string {
	for _, x := range Methods {
		if x.Value == m {
			return x.Label
		}
	}
	return ""
}

// Row is a raw hash-chained entries row (api.LedgerRow).
type Row struct {
	ID            int64
	Kind          string
	RefID         *int64
	Direction     string
	OccurredOn    *string
	AmountSen     *int64
	Currency      string
	CategoryID    *int64
	Payee         *string
	Method        *string
	Note          *string
	VoidReason    *string
	ReceiptSHA256 *string
	CreatedAt     string
	PrevHash      string
	Hash          string
}

// Entry is one row of the current view v_current (api.Entry).
// A voided entry keeps the field values of its last create/amend version.
type Entry struct {
	ID            int64 // original create id
	VersionID     int64 // latest create/amend row id
	Direction     string
	OccurredOn    string
	AmountSen     int64
	Currency      string
	CategoryID    int64
	CategoryName  string // CURRENT name from categories (not hashed; changes on rename)
	Payee         string
	Method        string
	Note          *string
	ReceiptSHA256 *string
	Status        string // active | amended | voided
	AmendCount    int
	VoidReason    *string
	CreatedAt     string // original create row
	UpdatedAt     string // latest row affecting the entry (amend or void)
	Hash          string // hash of that latest row
}

// Input is the body of a create or amend (api.EntryInput). Amend takes the
// full corrected version, not a patch.
type Input struct {
	Direction     string // "" = expense
	OccurredOn    string
	AmountSen     int64
	CategoryID    int64
	Payee         string
	Method        string
	Note          *string
	ReceiptSHA256 *string
	ConfirmDate   bool
}

// Request is one Append call.
type Request struct {
	Kind   string // create | amend | void
	RefID  int64  // original create id for amend/void
	Input  Input  // create/amend
	Reason string // void
}

// AppendResult is the committed row plus the updated current view (api.AppendResult).
type AppendResult struct {
	Row   Row
	Entry Entry
}

// Head identifies the chain tip.
type Head struct {
	ID   int64
	Hash string
}

// Problem is one bad row found by Verify.
//
// Detail disambiguates problems that are not about a single row's content:
// the receipt sha256 for "receipt mismatch"/"receipt missing", and the
// recorded head "id:hash" for "head mismatch". It is part of Key (used by
// acknowledgements) and may be shown to the owner.
type Problem struct {
	ID     int64
	Reason string
	Detail string
}

// Key identifies a problem for acknowledgement. It is stable across appends:
// head problems are keyed by the recorded head (not by the current tip id),
// receipt problems by sha256, everything else by (id, reason).
func (p Problem) Key() string {
	switch p.Reason {
	case ReasonHeadMismatch:
		return p.Reason + "|" + p.Detail
	case ReasonHeadMissing, ReasonHeadInvalid:
		return p.Reason
	case ReasonReceiptMismatch, ReasonReceiptMissing:
		return p.Reason + "|" + p.Detail
	}
	return strconv.FormatInt(p.ID, 10) + "|" + p.Reason
}

// VerifyResult mirrors api.VerifyResult (minus last_backup_at, which the API
// layer knows). On an empty ledger HeadID = 0 and HeadHash = GenesisHash.
type VerifyResult struct {
	OK           bool
	Entries      int64 // rows checked
	HeadID       int64
	HeadHash     string
	BrokenAt     *int64  // first bad row id when !OK
	Reason       *string // reason for BrokenAt
	CheckedAt    time.Time
	Acknowledged bool
	Problems     []Problem // every bad row found (capped at MaxProblems), ordered by id
}

// MaxProblems caps VerifyResult.Problems.
const MaxProblems = 1000

// Filter selects entries for List (all fields optional).
type Filter struct {
	From       string // YYYY-MM-DD inclusive on occurred_on
	To         string // YYYY-MM-DD inclusive
	CategoryID int64  // 0 = any
	Method     string // "" = any
	Q          string // case-insensitive substring of payee or note
	Status     string // "" or "all" | "active" (incl. amended) | "voided"
	Direction  string // "" or "all" | "expense" | "income" (FR-9)
}

// ListResult is api.EntryList. Totals exclude voided entries.
type ListResult struct {
	Entries     []Entry
	TotalSen    int64 // expenses only
	IncomeSen   int64 // income only (FR-9)
	NetSen      int64 // IncomeSen - TotalSen (may be negative)
	Count       int   // non-voided entries, both directions
	VoidedCount int
}

// CategoryTotal is one dashboard bar.
type CategoryTotal struct {
	CategoryID int64
	Name       string
	Color      *string
	TotalSen   int64
	Pct        float64 // display only, 1 decimal
}

// MonthTotal is one trend bar.
type MonthTotal struct {
	Month     string // YYYY-MM
	TotalSen  int64  // expenses
	IncomeSen int64  // income (FR-9)
}

// Summary is api.Summary. All totals exclude voided entries.
type Summary struct {
	Month              string
	TotalSen           int64 // expenses only
	Count              int   // non-voided entries in the month, both directions
	VoidedCount        int   // voided entries in the month, both directions
	IncomeSen          int64 // income only (FR-9)
	NetSen             int64 // IncomeSen - TotalSen (may be negative)
	PrevMonthTotalSen  int64 // expenses
	PrevMonthIncomeSen int64
	ChangePct          *float64        // expenses only; nil when prev month expense total is 0
	MissingReceipts    int             // non-voided EXPENSE entries without a receipt
	ByCategory         []CategoryTotal // expenses only; total desc, non-zero only
	Trend              []MonthTotal    // 6 months, oldest first, ending with Month
	Recent             []Entry         // 5 most recent overall, both directions, voided included
}
