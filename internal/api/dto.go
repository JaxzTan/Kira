// Package api holds Kira's HTTP layer. This file is the wire contract and mirrors
// docs/api-contract.md and web/src/api/types.ts. Only the main session edits it.
package api

// Money is always integer sen (int64). Never float.

// Enum values.
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
)

// Methods lists the payment methods in display order with their labels.
var Methods = []struct{ Value, Label string }{
	{MethodCard, "Card"},
	{MethodCash, "Cash"},
	{MethodBankTransfer, "Bank transfer"},
	{MethodTNGEWallet, "TNG eWallet"},
	{MethodGrabPay, "GrabPay"},
	{MethodOther, "Other"},
}

// Validation limits (TRD §7).
const (
	MaxAmountSen     int64 = 1_000_000_000 // exclusive: < RM 10,000,000
	MaxPayeeLen            = 120
	MaxNoteLen             = 500
	MaxVoidReasonLen       = 300
	MaxCategoryLen         = 60
	MaxReceiptBytes        = 10 << 20
	MaxJSONBodyBytes       = 64 << 10
	DateConfirmDays        = 365
)

// Error codes.
const (
	CodeBadRequest          = "bad_request"
	CodeUnauthorized        = "unauthorized"
	CodeForbidden           = "forbidden"
	CodeNotFound            = "not_found"
	CodeConflict            = "conflict"
	CodePayloadTooLarge     = "payload_too_large"
	CodeUnsupportedMedia    = "unsupported_media_type"
	CodeValidation          = "validation"
	CodeDateConfirmRequired = "date_confirm_required"
	CodeLedgerBroken        = "ledger_broken"
	CodeRateLimited         = "rate_limited"
	CodeInternal            = "internal"
)

// ErrorResponse is the body of every non-2xx JSON response.
type ErrorResponse struct {
	Error  string            `json:"error"`
	Code   string            `json:"code"`
	Fields map[string]string `json:"fields"`
}

// Entry is one row of the current view (v_current).
type Entry struct {
	ID            int64   `json:"id"`
	VersionID     int64   `json:"version_id"`
	Direction     string  `json:"direction"`
	OccurredOn    string  `json:"occurred_on"`
	AmountSen     int64   `json:"amount_sen"`
	Currency      string  `json:"currency"`
	CategoryID    int64   `json:"category_id"`
	CategoryName  string  `json:"category_name"`
	Payee         string  `json:"payee"`
	Method        string  `json:"method"`
	Note          *string `json:"note"`
	ReceiptSHA256 *string `json:"receipt_sha256"`
	Status        string  `json:"status"`
	AmendCount    int     `json:"amend_count"`
	VoidReason    *string `json:"void_reason"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	Hash          string  `json:"hash"`
}

// LedgerRow is a raw hash-chained row from the entries table.
type LedgerRow struct {
	ID            int64   `json:"id"`
	Kind          string  `json:"kind"`
	RefID         *int64  `json:"ref_id"`
	Direction     string  `json:"direction"`
	OccurredOn    *string `json:"occurred_on"`
	AmountSen     *int64  `json:"amount_sen"`
	Currency      string  `json:"currency"`
	CategoryID    *int64  `json:"category_id"`
	Payee         *string `json:"payee"`
	Method        *string `json:"method"`
	Note          *string `json:"note"`
	VoidReason    *string `json:"void_reason"`
	ReceiptSHA256 *string `json:"receipt_sha256"`
	CreatedAt     string  `json:"created_at"`
	PrevHash      string  `json:"prev_hash"`
	Hash          string  `json:"hash"`
}

// AppendResult is returned (201) by create, amend and void, only after commit.
type AppendResult struct {
	LedgerRow
	Entry Entry `json:"entry"`
}

// EntryInput is the body of POST /api/entries and POST /api/entries/:id/amend.
type EntryInput struct {
	Direction     string  `json:"direction,omitempty"` // default "expense"
	OccurredOn    string  `json:"occurred_on"`
	AmountSen     int64   `json:"amount_sen"`
	CategoryID    int64   `json:"category_id"`
	Payee         string  `json:"payee"`
	Method        string  `json:"method"`
	Note          *string `json:"note"`
	ReceiptSHA256 *string `json:"receipt_sha256"`
	ConfirmDate   bool    `json:"confirm_date,omitempty"`
}

// VoidInput is the body of POST /api/entries/:id/void.
type VoidInput struct {
	Reason string `json:"reason"`
}

// EntryList is the response of GET /api/entries.
type EntryList struct {
	Entries     []Entry `json:"entries"`
	TotalSen    int64   `json:"total_sen"`  // expenses only (non-voided)
	IncomeSen   int64   `json:"income_sen"` // income only (non-voided) — FR-9
	NetSen      int64   `json:"net_sen"`    // income_sen - total_sen
	Count       int     `json:"count"`
	VoidedCount int     `json:"voided_count"`
}

// EntryHistory is the response of GET /api/entries/:id/history.
type EntryHistory struct {
	Entry Entry       `json:"entry"`
	Rows  []LedgerRow `json:"rows"`
}

// Me is the response of GET /api/me.
type Me struct {
	Email   string  `json:"email"`
	Name    *string `json:"name"`
	Picture *string `json:"picture"`
}

// Receipt is the response of POST /api/receipts.
type Receipt struct {
	SHA256      string `json:"sha256"`
	Ext         string `json:"ext"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

// Category is a spending category.
type Category struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Color      *string `json:"color"`
	Icon       *string `json:"icon"`
	ArchivedAt *string `json:"archived_at"`
	Direction  string  `json:"direction"` // expense | income — FR-9
}

// CategoryList is the response of GET /api/categories.
type CategoryList struct {
	Categories []Category `json:"categories"`
}

// CategoryCreate is the body of POST /api/categories.
type CategoryCreate struct {
	Name      string  `json:"name"`
	Color     *string `json:"color"`
	Direction string  `json:"direction,omitempty"` // default "expense"
}

// CategoryPatch is the body of PATCH /api/categories/:id.
type CategoryPatch struct {
	Name     *string `json:"name"`
	Archived *bool   `json:"archived"`
}

// CategoryTotal is one bar in the dashboard breakdown.
type CategoryTotal struct {
	CategoryID int64   `json:"category_id"`
	Name       string  `json:"name"`
	Color      *string `json:"color"`
	TotalSen   int64   `json:"total_sen"`
	Pct        float64 `json:"pct"` // display percentage only; money stays in sen
}

// MonthTotal is one bar in the 6-month trend.
type MonthTotal struct {
	Month     string `json:"month"`
	TotalSen  int64  `json:"total_sen"`  // expenses
	IncomeSen int64  `json:"income_sen"` // income — FR-9
}

// Summary is the response of GET /api/summary.
type Summary struct {
	Month              string          `json:"month"`
	TotalSen           int64           `json:"total_sen"`
	Count              int             `json:"count"`
	VoidedCount        int             `json:"voided_count"`
	IncomeSen          int64           `json:"income_sen"` // FR-9
	NetSen             int64           `json:"net_sen"`    // income_sen - total_sen
	PrevMonthTotalSen  int64           `json:"prev_month_total_sen"`
	PrevMonthIncomeSen int64           `json:"prev_month_income_sen"`
	ChangePct          *float64        `json:"change_pct"`
	MissingReceipts    int             `json:"missing_receipts"`
	ByCategory         []CategoryTotal `json:"by_category"`
	Trend              []MonthTotal    `json:"trend"`
	Recent             []Entry         `json:"recent"`
}

// VerifyResult is the response of GET /api/ledger/verify and POST /api/ledger/acknowledge.
type VerifyResult struct {
	OK           bool    `json:"ok"`
	Entries      int64   `json:"entries"`
	HeadID       int64   `json:"head_id"`
	HeadHash     string  `json:"head_hash"`
	BrokenAt     *int64  `json:"broken_at"`
	Reason       *string `json:"reason"`
	CheckedAt    string  `json:"checked_at"`
	Acknowledged bool    `json:"acknowledged"`
	LastBackupAt *string `json:"last_backup_at"`
}

// CSVHeader is the exact export header (TR-10).
var CSVHeader = []string{"date", "category", "payee", "method", "amount_myr", "note", "status", "entry_id", "receipt_file", "type"}
