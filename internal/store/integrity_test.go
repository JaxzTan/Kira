package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIntegrityEvents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	at := time.Date(2026, 11, 9, 1, 2, 3, 0, time.UTC)
	rec, act := "5:"+strings.Repeat("a", 64), "4:"+strings.Repeat("b", 64)
	cases := []struct {
		name     string
		kind     string
		rec, act *string
		problems string
		wantErr  bool
	}{
		{"full", IntegrityAcknowledge, &rec, &act, `[{"id":5,"reason":"head mismatch"}]`, false},
		{"nil heads", IntegrityAcknowledge, nil, nil, `[]`, false},
		{"empty kind", "", nil, nil, `[]`, true},
		{"empty problems", IntegrityAcknowledge, nil, nil, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.InsertIntegrityEvent(ctx, at, tc.kind, tc.rec, tc.act, tc.problems)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v", err)
			}
		})
	}
	evs, err := s.RecentIntegrityEvents(ctx, 10)
	if err != nil || len(evs) != 2 {
		t.Fatalf("events = %+v, %v", evs, err)
	}
	if evs[0].RecordedHead != nil || evs[1].RecordedHead == nil || *evs[1].RecordedHead != rec || *evs[1].ActualHead != act || evs[1].At != "2026-11-09T01:02:03Z" {
		t.Fatalf("events = %+v", evs)
	}
	if evs, _ := s.RecentIntegrityEvents(ctx, 1); len(evs) != 1 || evs[0].ID != 2 {
		t.Fatalf("limit 1 = %+v", evs)
	}
	for _, q := range []string{
		`UPDATE integrity_events SET kind = 'x'`,
		`DELETE FROM integrity_events`,
		`INSERT OR REPLACE INTO integrity_events (id, at, kind, problems) VALUES (1, 'x', 'x', '[]')`,
	} {
		if _, err := s.DB().Exec(q); !IsAppendOnlyError(err) {
			t.Fatalf("%s: err = %v", q, err)
		}
	}
}

func TestUpdateCategoryAtomic(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	str := func(s string) *string { return &s }
	b := func(v bool) *bool { return &v }
	cases := []struct {
		name       string
		id         int64
		newName    *string
		archived   *bool
		wantErr    error
		wantName   string
		wantArchiv bool
	}{
		{"rename only", 1, str("  Cloud  "), nil, nil, "Cloud", false},
		{"archive only", 1, nil, b(true), nil, "Cloud & hosting", true},
		{"rename and archive", 1, str("Old cloud"), b(true), nil, "Old cloud", true},
		{"noop", 1, nil, nil, nil, "Cloud & hosting", false},
		{"duplicate name with archive: nothing applied", 1, str("hardware"), b(true), ErrDuplicateName, "Cloud & hosting", false},
		{"bad name with archive: nothing applied", 1, str("  "), b(true), ErrInvalid, "Cloud & hosting", false},
		{"missing", 999, str("x"), b(true), ErrNotFound, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := openTest(t)
			c, err := s.UpdateCategory(ctx, tc.id, tc.newName, tc.archived, at)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantName == "" {
				return
			}
			if err == nil && (c.Name != tc.wantName || (c.ArchivedAt != nil) != tc.wantArchiv) {
				t.Fatalf("returned %+v", c)
			}
			got, _ := s.GetCategory(ctx, tc.id)
			if got.Name != tc.wantName || (got.ArchivedAt != nil) != tc.wantArchiv {
				t.Fatalf("stored %+v", got)
			}
		})
	}
	// Unarchive keeps name; re-archive keeps original archived_at.
	s := openTest(t)
	c1, _ := s.UpdateCategory(ctx, 2, nil, b(true), at)
	c2, _ := s.UpdateCategory(ctx, 2, nil, b(true), at.Add(time.Hour))
	if *c1.ArchivedAt != *c2.ArchivedAt {
		t.Fatal("re-archive moved archived_at")
	}
	if c3, _ := s.UpdateCategory(ctx, 2, nil, b(false), at); c3.ArchivedAt != nil {
		t.Fatal("unarchive failed")
	}
}
