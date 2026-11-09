package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

var testTime = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)

func strp(v string) *string { return &v }
func boolp(v bool) *bool    { return &v }

// FR-9: categories carry an immutable direction.
func TestCategoryDirection(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	createCases := []struct {
		name, dir string
		wantDir   string
		wantErr   error
	}{
		{"Consulting", DirectionIncome, DirectionIncome, nil},
		{"Courses", "", DirectionExpense, nil},
		{"Books", DirectionExpense, DirectionExpense, nil},
		{"Weird", "refund", "", ErrInvalid},
		{"Income?", "INCOME", "", ErrInvalid},
		{"consulting", DirectionExpense, "", ErrDuplicateName}, // unique across directions
	}
	for _, tc := range createCases {
		t.Run("create "+tc.name, func(t *testing.T) {
			c, err := s.CreateCategory(ctx, tc.name, nil, tc.dir)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err == nil {
				if c.Direction != tc.wantDir {
					t.Fatalf("direction = %q, want %q", c.Direction, tc.wantDir)
				}
				if g, _ := s.GetCategory(ctx, c.ID); g.Direction != tc.wantDir {
					t.Fatalf("stored direction = %q", g.Direction)
				}
			}
			var ne *NameError
			if errors.Is(err, ErrInvalid) && (!errors.As(err, &ne) || ne.Field != "direction") {
				t.Fatalf("want NameError on direction, got %v", err)
			}
		})
	}
	// Archive one income seed so the includeArchived flag combines with direction.
	if _, err := s.ArchiveCategory(ctx, 14, testTime); err != nil {
		t.Fatal(err)
	}
	listCases := []struct {
		dir      string
		archived bool
		want     int
		wantErr  bool
	}{
		{"", true, 17, false}, // 10 + 4 seeds + 3 created
		{"all", false, 16, false},
		{DirectionExpense, true, 12, false},
		{DirectionExpense, false, 12, false},
		{DirectionIncome, true, 5, false},
		{DirectionIncome, false, 4, false},
		{"bogus", true, 0, true},
	}
	for _, tc := range listCases {
		cats, err := s.ListCategories(ctx, tc.archived, tc.dir)
		if (err != nil) != tc.wantErr {
			t.Fatalf("list %q: err = %v", tc.dir, err)
		}
		if tc.wantErr {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("list %q: want ErrInvalid, got %v", tc.dir, err)
			}
			continue
		}
		if len(cats) != tc.want {
			t.Fatalf("list %q archived=%v: got %d, want %d", tc.dir, tc.archived, len(cats), tc.want)
		}
		for _, c := range cats {
			if (tc.dir == DirectionExpense || tc.dir == DirectionIncome) && c.Direction != tc.dir {
				t.Fatalf("list %q returned %+v", tc.dir, c)
			}
		}
	}
	// Rename/archive keep direction; a raw UPDATE of direction is refused.
	c, err := s.UpdateCategory(ctx, 11, strp("Clients"), boolp(true), testTime)
	if err != nil || c.Direction != DirectionIncome {
		t.Fatalf("update: %+v %v", c, err)
	}
	for _, q := range []string{
		`UPDATE categories SET direction = 'expense' WHERE id = 11`,
		`UPDATE categories SET direction = 'income' WHERE id = 1`,
		`UPDATE categories SET direction = 'other' WHERE id = 1`,
	} {
		if _, err := s.DB().ExecContext(ctx, q); err == nil {
			t.Fatalf("%s: succeeded", q)
		}
	}
	// Same-value update (e.g. a full-row UPDATE) is allowed.
	if _, err := s.DB().ExecContext(ctx, `UPDATE categories SET direction = direction, name = name WHERE id = 1`); err != nil {
		t.Fatalf("no-op direction update: %v", err)
	}
}
