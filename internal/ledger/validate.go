package ledger

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Sentinel errors. Suggested HTTP mapping in comments.
var (
	ErrNotFound            = errors.New("entry not found")                                 // 404
	ErrNotCreate           = errors.New("target is not an original create entry")          // 409
	ErrVoided              = errors.New("entry is voided")                                 // 409
	ErrLedgerBroken        = errors.New("ledger verification failed; writes are disabled") // 423
	ErrDateConfirmRequired = errors.New("date is more than 1 year from today; confirm it") // 422 date_confirm_required
	ErrBadRequest          = errors.New("bad request")                                     // 400 (filters, month)
)

// ValidationError carries per-field messages (422 validation). Keys are
// API field names (amount_sen, occurred_on, category_id, method, payee,
// note, receipt_sha256, direction, reason).
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + " " + e.Fields[k]
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

func (e *ValidationError) add(field, msg string) {
	if e.Fields == nil {
		e.Fields = map[string]string{}
	}
	if _, ok := e.Fields[field]; !ok {
		e.Fields[field] = msg
	}
}

func (e *ValidationError) orNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IsSHA256Hex reports whether s is 64 lowercase hex characters.
func IsSHA256Hex(s string) bool { return shaRe.MatchString(s) }

// ValidMethod reports whether m is a method enum value.
func ValidMethod(m string) bool { return MethodLabel(m) != "" }

// ParseDate parses a strict YYYY-MM-DD date (UTC midnight).
func ParseDate(s string) (time.Time, error) {
	if len(s) != 10 {
		return time.Time{}, fmt.Errorf("bad date %q", s)
	}
	return time.Parse(time.DateOnly, s)
}

// ValidateAmount checks 0 < sen < MaxAmountSen.
func ValidateAmount(sen int64) error {
	switch {
	case sen <= 0:
		return errors.New("must be greater than 0")
	case sen >= MaxAmountSen:
		return errors.New("must be less than RM 10,000,000")
	}
	return nil
}

func checkText(s string, max int) string {
	switch {
	case !utf8.ValidString(s):
		return "must be valid UTF-8"
	case strings.ContainsRune(s, 0):
		return "must not contain NUL"
	case utf8.RuneCountInString(s) > max:
		return fmt.Sprintf("must be at most %d characters", max)
	}
	return ""
}

// ValidateInput normalizes and checks a create/amend body without touching
// the database (category and receipt existence are checked by Service,
// which merges those errors into the same *ValidationError so every failing
// field is reported at once). today is the server-local current date.
//
// Normalisation: direction "" = expense; payee and note are trimmed; a
// whitespace-only note becomes nil. receipt_sha256 must be nil or 64
// lowercase hex ("" is an error). direction "income" is rejected until FR-9.
//
// Returns *ValidationError for field errors; if fields are fine but the date
// is more than DateConfirmDays from today and !in.ConfirmDate, returns
// ErrDateConfirmRequired.
func ValidateInput(in Input, today time.Time) (Input, error) {
	out, ve, needConfirm := validateInput(in, today)
	if err := ve.orNil(); err != nil {
		return in, err
	}
	if needConfirm {
		return in, ErrDateConfirmRequired
	}
	return out, nil
}

// validateInput returns the normalised input, the field errors (possibly
// empty) and whether the date needs confirmation.
func validateInput(in Input, today time.Time) (Input, *ValidationError, bool) {
	ve := &ValidationError{}
	out := in
	if out.Direction == "" {
		out.Direction = DirectionExpense
	}
	switch out.Direction {
	case DirectionExpense:
	case DirectionIncome:
		ve.add("direction", "income is not supported yet; must be expense")
	default:
		ve.add("direction", "must be expense")
	}
	if err := ValidateAmount(out.AmountSen); err != nil {
		ve.add("amount_sen", err.Error())
	}
	d, dateErr := ParseDate(out.OccurredOn)
	if dateErr != nil {
		ve.add("occurred_on", "must be a valid date YYYY-MM-DD")
	}
	if out.CategoryID <= 0 {
		ve.add("category_id", "is required")
	}
	if !ValidMethod(out.Method) {
		ve.add("method", "must be one of card, cash, bank_transfer, tng_ewallet, grabpay, other")
	}
	out.Payee = strings.TrimSpace(out.Payee)
	if out.Payee == "" {
		ve.add("payee", "is required")
	} else if msg := checkText(out.Payee, MaxPayeeLen); msg != "" {
		ve.add("payee", msg)
	}
	if out.Note != nil {
		n := strings.TrimSpace(*out.Note)
		if n == "" {
			out.Note = nil
		} else if msg := checkText(n, MaxNoteLen); msg != "" {
			ve.add("note", msg)
		} else {
			out.Note = &n
		}
	}
	if out.ReceiptSHA256 != nil {
		if !IsSHA256Hex(*out.ReceiptSHA256) {
			ve.add("receipt_sha256", "must be null or 64 lowercase hex characters")
		} else {
			r := *out.ReceiptSHA256
			out.ReceiptSHA256 = &r
		}
	}
	needConfirm := false
	if dateErr == nil && !out.ConfirmDate {
		t := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
		days := int(d.Sub(t).Hours() / 24)
		needConfirm = days > DateConfirmDays || days < -DateConfirmDays
	}
	return out, ve, needConfirm
}

// ValidateReason trims and checks a void reason.
func ValidateReason(reason string) (string, error) {
	r := strings.TrimSpace(reason)
	ve := &ValidationError{}
	if r == "" {
		ve.add("reason", "is required")
	} else if msg := checkText(r, MaxVoidReasonLen); msg != "" {
		ve.add("reason", msg)
	}
	if err := ve.orNil(); err != nil {
		return "", err
	}
	return r, nil
}

// ValidateFilter checks list filters; errors wrap ErrBadRequest.
func ValidateFilter(f Filter) (Filter, error) {
	bad := func(msg string) (Filter, error) { return f, fmt.Errorf("%w: %s", ErrBadRequest, msg) }
	if f.From != "" {
		if _, err := ParseDate(f.From); err != nil {
			return bad("from must be YYYY-MM-DD")
		}
	}
	if f.To != "" {
		if _, err := ParseDate(f.To); err != nil {
			return bad("to must be YYYY-MM-DD")
		}
	}
	if f.Method != "" && !ValidMethod(f.Method) {
		return bad("unknown method")
	}
	if f.CategoryID < 0 {
		return bad("bad category")
	}
	switch f.Status {
	case "":
		f.Status = "all"
	case "all", StatusActive, StatusVoided:
	default:
		return bad("status must be all, active or voided")
	}
	f.Q = strings.TrimSpace(f.Q)
	return f, nil
}

// ParseMonth parses YYYY-MM; errors wrap ErrBadRequest.
func ParseMonth(s string) (time.Time, error) {
	if len(s) != 7 {
		return time.Time{}, fmt.Errorf("%w: month must be YYYY-MM", ErrBadRequest)
	}
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: month must be YYYY-MM", ErrBadRequest)
	}
	return t, nil
}
