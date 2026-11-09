package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxCategoryNameLen is the category name limit in characters (api-contract).
const MaxCategoryNameLen = 60

// Palette is the --cat-1..--cat-5 color cycle used for seeds and new categories.
var Palette = []string{"#0F766E", "#2F6FB5", "#C2701E", "#7A5BB0", "#9AA09B"}

var colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Category mirrors api.Category.
//
// Categories are NOT part of the ledger hash chain: entries reference
// category_id only, so renaming a category relabels historic entries and
// exports. Deferred to v1.1: snapshot category_name per entries row.
type Category struct {
	ID         int64
	Name       string
	Color      *string
	Icon       *string
	ArchivedAt *string
	Direction  string // DirectionExpense | DirectionIncome; set at creation, immutable (FR-9)
}

// Category directions (same strings as ledger entries' direction).
const (
	DirectionExpense = "expense"
	DirectionIncome  = "income"
)

// ValidDirection reports whether d is a category direction.
func ValidDirection(d string) bool { return d == DirectionExpense || d == DirectionIncome }

// NameError is returned for an invalid category field; Field is "name",
// "color" or "direction". It wraps ErrInvalid.
type NameError struct {
	Field string
	Msg   string
}

func (e *NameError) Error() string { return e.Field + ": " + e.Msg }
func (e *NameError) Unwrap() error { return ErrInvalid }

// NormalizeCategoryName trims and validates a category name.
func NormalizeCategoryName(name string) (string, error) {
	n := strings.TrimSpace(name)
	switch {
	case !utf8.ValidString(n):
		return "", &NameError{"name", "must be valid UTF-8"}
	case n == "":
		return "", &NameError{"name", "is required"}
	case utf8.RuneCountInString(n) > MaxCategoryNameLen:
		return "", &NameError{"name", fmt.Sprintf("must be at most %d characters", MaxCategoryNameLen)}
	}
	return n, nil
}

const catCols = `id, name, color, icon, archived_at, direction`

func scanCategory(sc interface{ Scan(...any) error }) (Category, error) {
	var c Category
	var color, icon, arch sql.NullString
	if err := sc.Scan(&c.ID, &c.Name, &color, &icon, &arch, &c.Direction); err != nil {
		return c, err
	}
	c.Color, c.Icon, c.ArchivedAt = nullStr(color), nullStr(icon), nullStr(arch)
	return c, nil
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

// ListCategories returns categories ordered by id; archived ones only if
// includeArchived. direction "" or "all" = both directions, otherwise
// DirectionExpense or DirectionIncome (anything else is a *NameError on
// "direction", wrapping ErrInvalid).
func (s *Store) ListCategories(ctx context.Context, includeArchived bool, direction string) ([]Category, error) {
	var conds []string
	var args []any
	switch direction {
	case "", "all":
	case DirectionExpense, DirectionIncome:
		conds, args = append(conds, `direction = ?`), append(args, direction)
	default:
		return nil, &NameError{"direction", "must be all, expense or income"}
	}
	if !includeArchived {
		conds = append(conds, `archived_at IS NULL`)
	}
	q := `SELECT ` + catCols + ` FROM categories`
	if len(conds) > 0 {
		q += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list categories: %w", err)
	}
	defer rows.Close()
	out := []Category{}
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCategory returns a category by id or ErrNotFound.
func (s *Store) GetCategory(ctx context.Context, id int64) (Category, error) {
	c, err := scanCategory(s.db.QueryRowContext(ctx, `SELECT `+catCols+` FROM categories WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, fmt.Errorf("store: get category: %w", err)
	}
	return c, nil
}

// nameTaken reports whether another category (id != exceptID) has name,
// compared with Unicode case folding (SQLite NOCASE only folds ASCII).
func (s *Store) nameTaken(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, name string, exceptID int64) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, name FROM categories`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return false, err
		}
		if id != exceptID && strings.EqualFold(n, name) {
			return true, nil
		}
	}
	return false, rows.Err()
}

// CreateCategory adds a category. color nil picks the next palette color.
// direction "" = DirectionExpense; it can never be changed afterwards (no
// store method updates it and a trigger refuses it).
// Errors: *NameError (ErrInvalid), ErrDuplicateName.
func (s *Store) CreateCategory(ctx context.Context, name string, color *string, direction string) (Category, error) {
	n, err := NormalizeCategoryName(name)
	if err != nil {
		return Category{}, err
	}
	if direction == "" {
		direction = DirectionExpense
	}
	if !ValidDirection(direction) {
		return Category{}, &NameError{"direction", "must be expense or income"}
	}
	if color != nil && !colorRe.MatchString(*color) {
		return Category{}, &NameError{"color", "must be #RRGGBB"}
	}
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE (DSN _txlock)
	if err != nil {
		return Category{}, fmt.Errorf("store: create category: %w", err)
	}
	defer tx.Rollback()
	taken, err := s.nameTaken(ctx, tx, n, 0)
	if err != nil {
		return Category{}, fmt.Errorf("store: create category: %w", err)
	}
	if taken {
		return Category{}, ErrDuplicateName
	}
	if color == nil {
		var count int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories`).Scan(&count); err != nil {
			return Category{}, err
		}
		c := Palette[count%int64(len(Palette))]
		color = &c
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO categories (name, color, direction) VALUES (?, ?, ?)`, n, *color, direction)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Category{}, ErrDuplicateName
		}
		return Category{}, fmt.Errorf("store: create category: %w", err)
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return Category{}, fmt.Errorf("store: create category: %w", err)
	}
	return s.GetCategory(ctx, id)
}

// RenameCategory changes a category's name. Prefer UpdateCategory for PATCH. Errors: ErrNotFound, *NameError, ErrDuplicateName.
func (s *Store) RenameCategory(ctx context.Context, id int64, name string) (Category, error) {
	n, err := NormalizeCategoryName(name)
	if err != nil {
		return Category{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Category{}, fmt.Errorf("store: rename category: %w", err)
	}
	defer tx.Rollback()
	taken, err := s.nameTaken(ctx, tx, n, id)
	if err != nil {
		return Category{}, fmt.Errorf("store: rename category: %w", err)
	}
	if taken {
		return Category{}, ErrDuplicateName
	}
	res, err := tx.ExecContext(ctx, `UPDATE categories SET name = ? WHERE id = ?`, n, id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Category{}, ErrDuplicateName
		}
		return Category{}, fmt.Errorf("store: rename category: %w", err)
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return Category{}, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return Category{}, fmt.Errorf("store: rename category: %w", err)
	}
	return s.GetCategory(ctx, id)
}

// UpdateCategory applies an optional rename and an optional archive flag
// change in ONE transaction, so a PATCH either fully applies or not at all.
// name nil = keep; archived nil = keep; *archived true archives (keeping an
// existing archived_at), false unarchives. With both nil it just returns the
// category. Errors: ErrNotFound, *NameError (ErrInvalid), ErrDuplicateName.
//
// Note: categories live outside the hash chain. Entries store category_id
// only, so a rename changes the label shown for historic entries (lists,
// export CSV). A v1.1 migration should snapshot category_name per row.
func (s *Store) UpdateCategory(ctx context.Context, id int64, name *string, archived *bool, at time.Time) (Category, error) {
	var n string
	if name != nil {
		var err error
		if n, err = NormalizeCategoryName(*name); err != nil {
			return Category{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE
	if err != nil {
		return Category{}, fmt.Errorf("store: update category: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM categories WHERE id = ?`, id).Scan(&exists); err != nil {
		return Category{}, fmt.Errorf("store: update category: %w", err)
	}
	if exists == 0 {
		return Category{}, ErrNotFound
	}
	if name != nil {
		taken, err := s.nameTaken(ctx, tx, n, id)
		if err != nil {
			return Category{}, fmt.Errorf("store: update category: %w", err)
		}
		if taken {
			return Category{}, ErrDuplicateName
		}
		if _, err := tx.ExecContext(ctx, `UPDATE categories SET name = ? WHERE id = ?`, n, id); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return Category{}, ErrDuplicateName
			}
			return Category{}, fmt.Errorf("store: update category: %w", err)
		}
	}
	if archived != nil {
		q, args := `UPDATE categories SET archived_at = NULL WHERE id = ?`, []any{id}
		if *archived {
			q, args = `UPDATE categories SET archived_at = COALESCE(archived_at, ?) WHERE id = ?`, []any{FormatTime(at), id}
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return Category{}, fmt.Errorf("store: update category: %w", err)
		}
	}
	c, err := scanCategory(tx.QueryRowContext(ctx, `SELECT `+catCols+` FROM categories WHERE id = ?`, id))
	if err != nil {
		return Category{}, fmt.Errorf("store: update category: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Category{}, fmt.Errorf("store: update category: %w", err)
	}
	return c, nil
}

// ArchiveCategory hides a category from pickers; old entries keep resolving its name.
// Archiving an already-archived category keeps the original archived_at.
func (s *Store) ArchiveCategory(ctx context.Context, id int64, at time.Time) (Category, error) {
	return s.setArchived(ctx, id, `UPDATE categories SET archived_at = COALESCE(archived_at, ?) WHERE id = ?`, FormatTime(at))
}

// UnarchiveCategory makes a category selectable again.
func (s *Store) UnarchiveCategory(ctx context.Context, id int64) (Category, error) {
	return s.setArchived(ctx, id, `UPDATE categories SET archived_at = NULL WHERE id = ?`)
}

func (s *Store) setArchived(ctx context.Context, id int64, q string, args ...any) (Category, error) {
	res, err := s.db.ExecContext(ctx, q, append(args, id)...)
	if err != nil {
		return Category{}, fmt.Errorf("store: archive category: %w", err)
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return Category{}, ErrNotFound
	}
	return s.GetCategory(ctx, id)
}
